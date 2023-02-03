package server_lib

import (
	"context"
	"sort"
	"log"
	"google.golang.org/grpc"
	"fmt"

	umc "cs426.yale.edu/lab1/user_service/mock_client"
	pb "cs426.yale.edu/lab1/video_rec_service/proto"
	vmc "cs426.yale.edu/lab1/video_service/mock_client"
	upb "cs426.yale.edu/lab1/user_service/proto"
	vpb "cs426.yale.edu/lab1/video_service/proto"
	"cs426.yale.edu/lab1/ranker"
	// "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/credentials/insecure"
)

type VideoRecServiceOptions struct {
	// Server address for the UserService"
	UserServiceAddr string
	// Server address for the VideoService
	VideoServiceAddr string
	// Maximum size of batches sent to UserService and VideoService
	MaxBatchSize int
	// If set, disable fallback to cache
	DisableFallback bool
	// If set, disable all retries
	DisableRetry bool
}

type VideoRecServiceServer struct {
	pb.UnimplementedVideoRecServiceServer
	options VideoRecServiceOptions
	// Add any data you want here
}

func MakeVideoRecServiceServer(options VideoRecServiceOptions) (*VideoRecServiceServer, error) {
	return &VideoRecServiceServer{
		options: options,
		// Add any data to initialize here
	}, nil
}

func MakeVideoRecServiceServerWithMocks(
	options VideoRecServiceOptions,
	mockUserServiceClient *umc.MockUserServiceClient,
	mockVideoServiceClient *vmc.MockVideoServiceClient,
) *VideoRecServiceServer {
	// Implement your own logic here
	return &VideoRecServiceServer{
		options: options,
		// ...
	}
}

func handleError(err error, message string) error {
	log.Printf("VideoRecService: %s, error %v\n", message, err)
	return status.Errorf(status.Code(err), "VideoRecService: %s, error %v\n", message, err)
}

func (server *VideoRecServiceServer) GetTopVideos(
	ctx context.Context,
	req *pb.GetTopVideosRequest,
) (*pb.GetTopVideosResponse, error) {

	// I. Fetch the user and users they subscribe to

	// (1) Create gRPC channel for UserService
	var optsUser []grpc.DialOption
	optsUser = append(optsUser, grpc.WithTransportCredentials(insecure.NewCredentials()))
	
	connUser, err := grpc.Dial(server.options.UserServiceAddr, optsUser...)
	if err != nil {
		return nil, handleError(err, "fail to dial")
	}
	defer connUser.Close()

	// (2) Create UserService client stub
	userClient := upb.NewUserServiceClient(connUser)

	// (3) Fetch user info for the original user
	orig_user_id := req.GetUserId()
	origUserResponse, err := userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: []uint64{orig_user_id}})
	if err != nil {
		return nil, handleError(err, "fail to fetch user info on orig user")
	}
	orig_user_infos := origUserResponse.GetUsers() // type []*UserInfo
	if len(orig_user_infos) != 1 {
		return nil, handleError(nil, fmt.Sprintf("incorrect number (%d) of UserInfos for orig user", len(orig_user_infos)))
	}
	orig_user_info := orig_user_infos[0]

	// (4) Fetch users that the orig user subscribes to
	subscribed_to := orig_user_info.GetSubscribedTo()

	// (5) Fetch the liked videos of the subscribe users
	subscribed_user_infos := make([]*upb.UserInfo, 0)
	for _, s := range subscribed_to {
		likedVideoResponse, err := userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: []uint64{s}})
		if err != nil {
			return nil, handleError(err, "fail to fetch liked videos")
		}

		subscribed_user_infos = append(subscribed_user_infos, likedVideoResponse.GetUsers()...)
	}

	liked_videos := make([]uint64, 0)
	liked_videos_map := make(map[uint64]bool) // to make sure there are no duplicates

	for _, subscribed_user_info := range subscribed_user_infos {
		vids := subscribed_user_info.GetLikedVideos()
		for _, v := range vids {
			if _, contains := liked_videos_map[v]; !contains {
				liked_videos_map[v] = true
				liked_videos = append(liked_videos, v)
			}
		}
	}

	// II. Fetch the video infos for the liked videos

	// (1) Create gRPC channel for VideoService
	var optsVideo []grpc.DialOption
	optsVideo = append(optsVideo, grpc.WithTransportCredentials(insecure.NewCredentials()))

	connVideo, err := grpc.Dial(server.options.VideoServiceAddr, optsVideo...)
	if err != nil {
		return nil, handleError(err, "fail to dial")
	}
	defer connVideo.Close()

	// (2) Create VideoService client stub
	videoClient := vpb.NewVideoServiceClient(connVideo)

	// (3) Fetch video infos for liked videos
	video_infos := make([]*vpb.VideoInfo, 0)
	for _, v := range liked_videos {
		videoResponse, err := videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: []uint64{v}})
		if err != nil {
			return nil, handleError(err, "fail to fetch video infos")
		}

		video_infos = append(video_infos, videoResponse.GetVideos()...)
	}

	// III. Rank videos

	// (1) Create instance of ranker
	ranker := ranker.BcryptRanker{}

	// (2) Fetch original user's UserCoefficient
	orig_user_coefficient := orig_user_info.GetUserCoefficients()

	// (3) Rank liked videos
	liked_videos_ranked := make([]*vpb.VideoInfo, 0)
	liked_videos_ranked_map := make(map[*vpb.VideoInfo]uint64)

	for _, v := range video_infos {
		// Compute rank for v
		video_coefficient := v.GetVideoCoefficients()
		rank := ranker.Rank(orig_user_coefficient, video_coefficient)

		liked_videos_ranked = append(liked_videos_ranked, v)
		liked_videos_ranked_map[v] = rank
	}

	sort.SliceStable(liked_videos_ranked, func(i, j int) bool {
        return liked_videos_ranked_map[liked_videos_ranked[i]] > liked_videos_ranked_map[liked_videos_ranked[j]]
    })

	// (4) Truncate the list
	limit := req.GetLimit()
	if limit > 0 && limit <= int32(len(liked_videos_ranked)) {
		liked_videos_ranked = liked_videos_ranked[:limit]
	}

	return &pb.GetTopVideosResponse{Videos: liked_videos_ranked}, nil
}
