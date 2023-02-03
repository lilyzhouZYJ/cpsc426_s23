package server_lib

import (
	"context"

	umc "cs426.yale.edu/lab1/user_service/mock_client"
	pb "cs426.yale.edu/lab1/video_rec_service/proto"
	vmc "cs426.yale.edu/lab1/video_service/mock_client"
	upb "cs426.yale.edu/lab1/user_service/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func (server *VideoRecServiceServer) GetTopVideos(
	ctx context.Context,
	req *pb.GetTopVideosRequest,
) (*pb.GetTopVideosResponse, error) {

	// Create gRPC channel
	conn, err := grpc.Dial(server.options.UserServiceAddr, opts...)
	if err != nil {
		log.Fatalf("VideoRecService: fail to dial with error %v\n", err)
		return nil, status.Errorf(
			status.Code(err),
			"VideoRecService: error %v\n",
			err
		)
	}
	defer conn.Close()

	// Create client stub
	client := pb.NewRouteGuideClient(conn)

	// Create user client stub
	userClient := upb.NewUserServiceClient(conn)

	// Fetch user info on the original user
	orig_user_id := req.GetUserId()
	origUserResponse, err := userClient.GetUser(ctx, &upb.GetUserRequest{user_ids: orig_user_id})
	if err != nil {
		log.Fatalf("VideoRecService: fail to fetch user info on orig user with error %v", err)
		return nil, status.Errorf(
			status.Code(err),
			"VideoRecService: error %v\n",
			err
		)
	}
	orig_user_infos []*UserInfo := origUserResponse.GetUsers()

	// Fetch users that the orig user subscribes to
	subscribed_to := make(uint64[], 0)
	for _, orig_user_info := range orig_user_infos {
		s := orig_user_info.GetSubscribedTo()
		subscribed_to := append(subscribed_to, s)
	}

	// Find the liked videos of the subscribe users
	likedVideoResponse, err := userClient.GetUser(ctx, &upb.GetUserRequest{user_ids: subscribed_to})
	if err != nil {
		log.Fatalf("VideoRecService: fail to fetch liked videos with error %v", err)
		return nil, status.Errorf(
			status.Code(err),
			"VideoRecService: error %v\n",
			err
		)
	}
	subscribed_user_infos []*UserInfo := likedVideoResponse.GetUsers()

	liked_videos := make(uint64[], 0)
	liked_videos_map := make(map[int]bool) // to make sure there are no duplicates

	for _, subscribed_user_info := range subscribed_user_infos {
		vids := subscribed_user_info.GetLikedVideos()
		for _, v := range vids {
			if _, contains := liked_videos_map[v]; !contains {
				liked_videos_map[v] = true
				liked_videos = append(liked_videos, v)
			}
		}
	}
}
