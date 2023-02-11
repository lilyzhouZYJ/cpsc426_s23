package server_lib

import (
	"context"
	"sort"
	"log"
	"google.golang.org/grpc"
	// "fmt"
	"sync"
	"sync/atomic"
	"time"

	umc "cs426.yale.edu/lab1/user_service/mock_client"
	pb "cs426.yale.edu/lab1/video_rec_service/proto"
	vmc "cs426.yale.edu/lab1/video_service/mock_client"
	upb "cs426.yale.edu/lab1/user_service/proto"
	vpb "cs426.yale.edu/lab1/video_service/proto"
	vsl "cs426.yale.edu/lab1/video_service/server_lib"
	"cs426.yale.edu/lab1/ranker"
	"google.golang.org/grpc/codes"
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

	// ClientConns
	userConn *grpc.ClientConn
	videoConn *grpc.ClientConn
	
	useMock bool
	mockUserServiceClient *umc.MockUserServiceClient
	mockVideoServiceClient *vmc.MockVideoServiceClient

	// Stats
	numTotalRequests uint64
	numTotalErrors uint64
	numActiveRequests uint64
	numUserErrors uint64
	numVideoErrors uint64
	totalLatency uint64
	numStaleResponses uint64

	lock sync.RWMutex

	// Cache trending videos
	trendingLock sync.RWMutex
	trendingVideos []*vpb.VideoInfo
	expirationTime uint64
}

func MakeVideoRecServiceServer(options VideoRecServiceOptions) (*VideoRecServiceServer, error) {
	// Dial to create ClientConns
	userConn, err := CreateConnection(options.DisableRetry, options.UserServiceAddr)
	if err != nil {
		return nil, handleError(codes.Unavailable, "fail to dial to UserService")
	}
	videoConn, err := CreateConnection(options.DisableRetry, options.VideoServiceAddr)
	if err != nil {
		return nil, handleError(codes.Unavailable, "fail to dial to VideoService")
	}

	server := &VideoRecServiceServer{
		options: options,
		userConn: userConn,
		videoConn: videoConn,
		useMock: false,
		trendingVideos: nil,
		expirationTime: uint64(time.Now().Unix()),
	}

	// Update trending videos in a goroutine
	if !server.options.DisableFallback {
		// log.Println("start goroutine in MakeVideoRecServiceServer")
		go UpdateTrendingVideos(server)
	}

	return server, nil
}

func MakeVideoRecServiceServerWithMocks(
	options VideoRecServiceOptions,
	mockUserServiceClient *umc.MockUserServiceClient,
	mockVideoServiceClient *vmc.MockVideoServiceClient,
) *VideoRecServiceServer {

	server := &VideoRecServiceServer{
		options: options,

		useMock: true,
		mockUserServiceClient: mockUserServiceClient,
		mockVideoServiceClient: mockVideoServiceClient,

		trendingVideos: nil,
		expirationTime: uint64(time.Now().Unix()),
	}

	// Update trending videos in a goroutine
	if !server.options.DisableFallback {
		// log.Println("start goroutine in MakeVideoRecServiceServerWithMocks")
		go UpdateTrendingVideos(server)
	}

	return server
}

// Helper function
func CreateConnection(disableRetry bool, addr string) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		// Retry
		if !disableRetry {
			conn, err = grpc.Dial(addr, opts...)
		}

		if err != nil {
			return nil, err
		}
	}

	return conn, err
}

// Internal helper for UpdateTrendingVideos();
// the thread should already hold the trendingLock
func UpdateTrendingVideosInternal(
	server *VideoRecServiceServer,
	videoClient vpb.VideoServiceClient,
	batchSize int,
) error {
	// Create context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// (1) Fetch trending videos
	trendingVideosResponse, err := videoClient.GetTrendingVideos(ctx, &vpb.GetTrendingVideosRequest{})
	if err != nil {
		// Retry
		trendingVideosResponse, err = videoClient.GetTrendingVideos(ctx, &vpb.GetTrendingVideosRequest{})
		if err != nil {
			return handleError(codes.Internal, "fail to fetch video ids for trending videos")
		}
	}
	videoIds := trendingVideosResponse.GetVideos()
	expirationTime := trendingVideosResponse.GetExpirationTimeS()

	// (2) Fetch video infos for videoIds
	// log.Println("fetching video infos for trending videos")
	videoInfos, err := FetchVideoInfos(ctx, server, videoClient, videoIds)
	if err != nil {
		return handleError(codes.Internal, "fail to fetch video infos for trending videos")
	}

	// (3) Update server cache
	if expirationTime > server.expirationTime {
		// Only update if the fetched videos have a later expiration time
		server.trendingVideos = videoInfos
		server.expirationTime = expirationTime
	}

	return nil
}

func UpdateTrendingVideos(server *VideoRecServiceServer){
	// Max batch size
	batchSize := server.options.MaxBatchSize

	// VideoService client
	var videoClient vpb.VideoServiceClient = nil

	// Infinite loop
	for {
		var err error

		// Check if there is no videoClient
		if videoClient == nil {
			if server.useMock {
				// log.Println("update trending videos with mock")
				videoClient = vmc.MakeMockVideoServiceClient(*vsl.DefaultVideoServiceOptions())
				// vOptions := vsl.VideoServiceOptions{MaxBatchSize: 50}
				// videoClient = vmc.MakeMockVideoServiceClient(vOptions)
			} else {
				// Create gRPC channel for VideoService
				var connVideo *grpc.ClientConn
				connVideo, err = CreateConnection(server.options.DisableRetry, server.options.VideoServiceAddr)
				if err == nil {
					defer connVideo.Close()
					videoClient = vpb.NewVideoServiceClient(connVideo)
				}
			}
		}

		if videoClient != nil {
			// If creating videoClient succeeded
			server.trendingLock.Lock()
			if uint64(time.Now().Unix()) >= server.expirationTime - 20 {
				// Previous cache has expired; fetch update
				err = UpdateTrendingVideosInternal(server, videoClient, batchSize)
			}
			server.trendingLock.Unlock()
		}

		if err != nil {
			// If we encountered failure, back off and wait
			time.Sleep(10 * time.Second)
		}
	}	
}

func GetCachedTrendingVideos(server *VideoRecServiceServer, limit int32) ([]*vpb.VideoInfo, bool) {
	server.trendingLock.RLock()
	defer server.trendingLock.RUnlock()

	if server.trendingVideos == nil {
		return nil, false
	}

	// Truncate to limit
	videos := server.trendingVideos
	if limit > 0 && limit <= int32(len(videos)) {
		videos = videos[:limit]
	}
	
	if time.Now().Unix() < int64(server.expirationTime) {
		// Cached videos has not expired
		log.Printf("VideoRecService: getting cached trending videos (expired)")
		return videos, false
	} else {
		// Cached videos has expired
		log.Printf("VideoRecService: getting cached trending videos (unexpired)")
		return videos, true
	}
}

func handleError(code codes.Code, message string) error {
	log.Printf("VideoRecService: %s, error code %d\n", message, code)
	return status.Errorf(code, "VideoRecService: %s, error code %d\n", message, code)
}

func updateStats(
	server *VideoRecServiceServer,
	startTime time.Time,
	hasError bool,
	userError bool,
	videoError bool,
	useStale bool,
) {
	// Lock
	server.lock.Lock()
	defer server.lock.Unlock()

	// Update stats
	server.numTotalRequests += 1

	if hasError {
		server.numTotalErrors += 1
		if userError {
			server.numUserErrors += 1
		}
		if videoError {
			server.numVideoErrors += 1
		}
	}

	server.totalLatency += uint64(time.Since(startTime).Milliseconds())
	server.numActiveRequests -= 1

	if useStale {
		server.numStaleResponses += 1
	}
}

// Helper function
func FetchUserInfos(
	ctx context.Context,
	server *VideoRecServiceServer,
	userClient upb.UserServiceClient,
	userIds []uint64, 
) ([]*upb.UserInfo, error) {

	maxBatchSize := server.options.MaxBatchSize
	userInfos := make([]*upb.UserInfo, 0)

	if maxBatchSize == 0 {
		// No batch
		for i := 0; i < len(userIds); i++ {
			userResponse, err := userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: []uint64{userIds[i]}})
			if err != nil {
				// Retry
				if !server.options.DisableRetry {
					userResponse, err = userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: []uint64{userIds[i]}})
				}

				if err != nil {
					return nil, err
				}
			}

			userInfos = append(userInfos, userResponse.GetUsers()...)
		}
	} else {
		// Batch
		for len(userIds) > 0 {
			// Slice to fix into batch size
			batch := make([]uint64, 0)
	
			if len(userIds) > maxBatchSize {
				batch = userIds[:maxBatchSize]
				userIds = userIds[maxBatchSize:]
			} else {
				batch = userIds
				userIds = make([]uint64, 0)
			}
	
			userResponse, err := userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: batch})
			if err != nil {
				// Retry
				if !server.options.DisableRetry {
					userResponse, err = userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: batch})
				}
	
				if err != nil {
					return nil, err
				}
			}
	
			userInfos = append(userInfos, userResponse.GetUsers()...)
		}
	}

	return userInfos, nil
}

// Helper function
func FetchVideoInfos(
	ctx context.Context,
	server *VideoRecServiceServer,
	videoClient vpb.VideoServiceClient,
	videoIds []uint64, 
) ([]*vpb.VideoInfo, error) {

	maxBatchSize := server.options.MaxBatchSize
	videoInfos := make([]*vpb.VideoInfo, 0)

	// log.Println("FetchVideoInfos with videoIds:", videoIds)

	if maxBatchSize == 0 {
		// No batch
		for i := 0; i < len(videoIds); i++ {
			videoResponse, err := videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: []uint64{videoIds[i]}})
			if err != nil {
				// Retry
				if !server.options.DisableRetry {
					videoResponse, err = videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: []uint64{videoIds[i]}})
				}

				if err != nil {
					return nil, err
				}
			}

			videoInfos = append(videoInfos, videoResponse.GetVideos()...)
		}
	} else {
		for len(videoIds) > 0 {
			// Slice to fix into batch size
			batch := make([]uint64, 0)
	
			if len(videoIds) > maxBatchSize {
				batch = videoIds[:maxBatchSize]
				videoIds = videoIds[maxBatchSize:]
			} else {
				batch = videoIds
				videoIds = make([]uint64, 0)
			}
	
			videoResponse, err := videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: batch})
			if err != nil {
				// Retry
				if !server.options.DisableRetry {
					videoResponse, err = videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: batch})
				}
	
				if err != nil {
					// log.Println("error")
					return nil, err
				}
			}
	
			videoInfos = append(videoInfos, videoResponse.GetVideos()...)
		}
	}

	return videoInfos, nil
}

func (server *VideoRecServiceServer) GetTopVideos(
	ctx context.Context,
	req *pb.GetTopVideosRequest,
) (*pb.GetTopVideosResponse, error) {

	// Update stats
	startTime := time.Now()
	atomic.AddUint64(&server.numActiveRequests, 1)

	// I. Fetch the user and users they subscribe to

	// (1) Create UserService clients
	var userClient upb.UserServiceClient
	if server.useMock {
		// Use the mock client
		userClient = server.mockUserServiceClient
	} else {
		// Create UserService client stub
		userClient = upb.NewUserServiceClient(server.userConn)
	}
	
	// (2) Fetch user info for the original user
	origUserId := req.GetUserId()
	origUserInfos, err := FetchUserInfos(ctx, server, userClient, []uint64{origUserId})
	if err != nil {
		// Fallback
		if !server.options.DisableFallback {
			cachedVideos, _ := GetCachedTrendingVideos(server, req.GetLimit())
			if cachedVideos != nil {
				defer updateStats(server, startTime, false, false, false, true)
				return &pb.GetTopVideosResponse{Videos: cachedVideos, StaleResponse: true}, nil
			}
		}

		defer updateStats(server, startTime, true, true, false, false)
		return nil, handleError(codes.Internal, "fail to fetch user info on orig user")
	}
	origUserInfo := origUserInfos[0]

	// (3) Fetch users that the orig user subscribes to
	subscribedTo := origUserInfo.GetSubscribedTo()

	// (4) Fetch the UserInfos of subscribe-to users
	subscribedUserInfos, err := FetchUserInfos(ctx, server, userClient, subscribedTo)
	if err != nil {
		// Fallback
		if !server.options.DisableFallback {
			cachedVideos, _ := GetCachedTrendingVideos(server, req.GetLimit())
			if cachedVideos != nil {
				defer updateStats(server, startTime, false, false, false, true)
				return &pb.GetTopVideosResponse{Videos: cachedVideos, StaleResponse: true}, nil
			}
		}

		defer updateStats(server, startTime, true, true, false, false)
		return nil, handleError(codes.Internal, "fail to fetch liked videos of the subscribe users")		
	}

	// (5) Fetch the liked videos of subscribe-to users
	likedVideos := make([]uint64, 0)
	LikedVideosMap := make(map[uint64]bool) // to make sure there are no duplicates

	for _, userInfo := range subscribedUserInfos {
		vids := userInfo.GetLikedVideos()
		for _, v := range vids {
			if _, contains := LikedVideosMap[v]; !contains {
				LikedVideosMap[v] = true
				likedVideos = append(likedVideos, v)
			}
		}
	}

	// II. Fetch the video infos for the liked videos

	// (1) Create VideoService client
	var videoClient vpb.VideoServiceClient
	if server.useMock {
		videoClient = server.mockVideoServiceClient
	} else {
		// Create VideoService client stub
		videoClient = vpb.NewVideoServiceClient(server.videoConn)
	}
	
	// (2) Fetch video infos for liked videos
	// log.Println(likedVideos)
	videoInfos, err := FetchVideoInfos(ctx, server, videoClient, likedVideos)
	if err != nil {
		// Fallback
		if !server.options.DisableFallback {
			cachedVideos, _ := GetCachedTrendingVideos(server, req.GetLimit())
			if cachedVideos != nil {
				defer updateStats(server, startTime, false, false, false, true)
				return &pb.GetTopVideosResponse{Videos: cachedVideos, StaleResponse: true}, nil
			}
		}

		defer updateStats(server, startTime, true, false, true, false)
		return nil, handleError(codes.Internal, "fail to fetch video infos of liked videos")
	}

	// III. Rank videos

	// (1) Create instance of ranker
	ranker := ranker.BcryptRanker{}

	// (2) Fetch original user's UserCoefficient
	origUserCoefficient := origUserInfo.GetUserCoefficients()

	// (3) Rank liked videos
	likedVideosRanked := make([]*vpb.VideoInfo, 0)
	likedVideosRankedMap := make(map[*vpb.VideoInfo]uint64)

	for _, v := range videoInfos {
		// Compute rank for v
		videoCoefficient := v.GetVideoCoefficients()
		rank := ranker.Rank(origUserCoefficient, videoCoefficient)

		likedVideosRanked = append(likedVideosRanked, v)
		likedVideosRankedMap[v] = rank
	}

	sort.SliceStable(likedVideosRanked, func(i, j int) bool {
        return likedVideosRankedMap[likedVideosRanked[i]] > likedVideosRankedMap[likedVideosRanked[j]]
    })

	// (4) Truncate the list
	limit := req.GetLimit()
	if limit > 0 && limit <= int32(len(likedVideosRanked)) {
		likedVideosRanked = likedVideosRanked[:limit]
	}

	defer updateStats(server, startTime, false, false, false, false)
	return &pb.GetTopVideosResponse{Videos: likedVideosRanked}, nil
}

func (server *VideoRecServiceServer) GetStats(
	ctx context.Context,
	req *pb.GetStatsRequest,
) (*pb.GetStatsResponse, error) {

	server.lock.Lock()
	defer server.lock.Unlock()
	
	response := pb.GetStatsResponse{
		TotalRequests: server.numTotalRequests,
		TotalErrors: server.numTotalErrors,
		ActiveRequests: server.numActiveRequests,
		UserServiceErrors: server.numUserErrors,
		VideoServiceErrors: server.numVideoErrors,
		AverageLatencyMs: float32(float64(server.totalLatency) / float64(server.numTotalRequests)),
		StaleResponses: server.numStaleResponses,
	}

	return &response, nil
}