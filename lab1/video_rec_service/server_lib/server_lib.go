package server_lib

import (
	"context"
	"sort"
	"log"
	"google.golang.org/grpc"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

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
	trendingInitialized bool
	trendingVideos []*vpb.VideoInfo
	expirationTime uint64
}

func MakeVideoRecServiceServer(options VideoRecServiceOptions) (*VideoRecServiceServer, error) {
	return &VideoRecServiceServer{
		options: options,
		useMock: false,
		trendingVideos: make([]*vpb.VideoInfo, 0),
		expirationTime: uint64(time.Now().Unix()),
	}, nil
}

func MakeVideoRecServiceServerWithMocks(
	options VideoRecServiceOptions,
	mockUserServiceClient *umc.MockUserServiceClient,
	mockVideoServiceClient *vmc.MockVideoServiceClient,
) *VideoRecServiceServer {

	return &VideoRecServiceServer{
		options: options,

		useMock: true,
		mockUserServiceClient: mockUserServiceClient,
		mockVideoServiceClient: mockVideoServiceClient,

		trendingVideos: make([]*vpb.VideoInfo, 0),
		expirationTime: uint64(time.Now().Unix()),
	}
}

func UpdateTrendingVideosInternal(
	ctx context.Context,
	server *VideoRecServiceServer,
	batchSize int,
) error {

	// (1) Create VideoService client
	var videoClient vpb.VideoServiceClient
	if server.useMock {
		videoClient = server.mockVideoServiceClient
	} else {
		// Create gRPC channel for VideoService
		var optsVideo []grpc.DialOption
		optsVideo = append(optsVideo, grpc.WithTransportCredentials(insecure.NewCredentials()))

		connVideo, err := grpc.Dial(server.options.VideoServiceAddr, optsVideo...)
		if err != nil {
			// Retry
			connVideo, err = grpc.Dial(server.options.VideoServiceAddr, optsVideo...)
			if err != nil {
				return handleError(err, "fail to dial")
			}
		}
		defer connVideo.Close()

		// Create VideoService client stub
		videoClient = vpb.NewVideoServiceClient(connVideo)
	}

	// (2) Fetch trending videos
	trendingVideosResponse, err := videoClient.GetTrendingVideos(ctx, &vpb.GetTrendingVideosRequest{})
	if err != nil {
		// Retry
		trendingVideosResponse, err = videoClient.GetTrendingVideos(ctx, &vpb.GetTrendingVideosRequest{})
		if err != nil {
			return handleError(err, "fail to fetch trending videos")
		}
	}
	videoIds := trendingVideosResponse.GetVideos()
	expirationTime := trendingVideosResponse.GetExpirationTimeS()

	// (3) Fetch video infos for videoIds
	videoInfos := make([]*vpb.VideoInfo, 0)

	// Batching:
	if batchSize > 0 {
		// Batching
		for len(videoIds) > 0 {
			// Slice for next request
			vids := make([]uint64, 0)

			if len(videoIds) > batchSize {
				vids = videoIds[:batchSize]
				videoIds = videoIds[batchSize:]
			} else {
				vids = videoIds
				videoIds = make([]uint64, 0)
			}

			videoResponse, err := videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: vids})
			if err != nil {
				// Retry
				videoResponse, err = videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: vids})
				if err != nil {
					return handleError(err, "fail to fetch video infos")
				}
			}
	
			videoInfos = append(videoInfos, videoResponse.GetVideos()...)
		}
	} else {
		// No batching
		for _, v := range videoIds {
			videoResponse, err := videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: []uint64{v}})
			if err != nil {
				// Retry
				videoResponse, err = videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: []uint64{v}})
				if err != nil {
					return handleError(err, "fail to fetch video infos")
				}
			}
	
			videoInfos = append(videoInfos, videoResponse.GetVideos()...)
		}
	}

	// (4) Update server cache
	server.trendingLock.Lock()
	defer server.trendingLock.Unlock()

	if expirationTime > server.expirationTime {
		// Only update if the fetched videos have a later expiration time
		server.trendingVideos = videoInfos
		server.expirationTime = expirationTime
	}

	return nil
}

func UpdateTrendingVideos(
	ctx context.Context,
	server *VideoRecServiceServer,
){
	// Max batch size
	batchSize := server.options.MaxBatchSize

	for {
		if uint64(time.Now().Unix()) >= server.expirationTime - 30 {
			// Previous cache has expired; fetch update
			UpdateTrendingVideosInternal(ctx, server, batchSize)
		}
		time.Sleep(10 * time.Second)
	}	
}

func GetCachedTrendingVideos(server *VideoRecServiceServer) ([]*vpb.VideoInfo, bool) {
	server.trendingLock.RLock()
	defer server.trendingLock.RUnlock()

	if !server.trendingInitialized {
		return nil, false
	}
	
	if time.Now().Unix() < int64(server.expirationTime) {
		// Cached videos has not expired
		log.Printf("VideoRecService: getting cached trending videos (expired)")
		return server.trendingVideos, false
	} else {
		// Cached videos has expired
		log.Printf("VideoRecService: getting cached trending videos (unexpired)")
		return server.trendingVideos, true
	}
}

func handleError(err error, message string) error {
	log.Printf("VideoRecService: %s, error %v\n", message, err)
	return status.Errorf(status.Code(err), "VideoRecService: %s, error %v\n", message, err)
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

	log.Printf("USESTALE %d", useStale)
	if useStale {
		log.Printf("USING STALE")
		server.numStaleResponses += 1
		log.Printf("STALE COUNT %d", server.numStaleResponses)
	}
}

func (server *VideoRecServiceServer) GetTopVideos(
	ctx context.Context,
	req *pb.GetTopVideosRequest,
) (*pb.GetTopVideosResponse, error) {

	// Update stats
	startTime := time.Now()
	atomic.AddUint64(&server.numActiveRequests, 1)

	// Fetch trending videos and update cache
	server.trendingLock.Lock()
	if !server.trendingInitialized && !server.options.DisableFallback {
		go UpdateTrendingVideos(ctx, server)
		server.trendingInitialized = true
	}
	server.trendingLock.Unlock()

	// I. Fetch the user and users they subscribe to

	// (1) Create UserService clients
	var userClient upb.UserServiceClient
	if server.useMock {
		// Use the mock client
		userClient = server.mockUserServiceClient
	} else {
		// Create gRPC channel for UserService
		var optsUser []grpc.DialOption
		optsUser = append(optsUser, grpc.WithTransportCredentials(insecure.NewCredentials()))
		
		connUser, err := grpc.Dial(server.options.UserServiceAddr, optsUser...)
		if err != nil {
			// Retry
			if !server.options.DisableRetry {
				connUser, err = grpc.Dial(server.options.UserServiceAddr, optsUser...)
				if err != nil {
					if !server.options.DisableFallback {
						// Use fallback
						cachedVideos, _ := GetCachedTrendingVideos(server)
						if cachedVideos != nil {
							defer updateStats(server, startTime, false, false, false, true)
							return &pb.GetTopVideosResponse{Videos: cachedVideos}, nil
						}
					}
				}
			}
			
			defer updateStats(server, startTime, true, true, false, false)
			return nil, handleError(err, "fail to dial")
		}
		defer connUser.Close()

		// Create UserService client stub
		userClient = upb.NewUserServiceClient(connUser)
	}
	
	// (2) Fetch user info for the original user
	orig_user_id := req.GetUserId()	
	origUserResponse, err := userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: []uint64{orig_user_id}})
	if err != nil {
		// Retry
		if server.options.DisableRetry {
			origUserResponse, err = userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: []uint64{orig_user_id}})
			if err != nil {
				if !server.options.DisableFallback {
					// Use fallback
					cachedVideos, _ := GetCachedTrendingVideos(server)
					if cachedVideos != nil {
						defer updateStats(server, startTime, false, false, false, true)
						return &pb.GetTopVideosResponse{Videos: cachedVideos}, nil
					}
				}
			}
		}

		defer updateStats(server, startTime, true, true, false, false)
		return nil, handleError(err, "fail to fetch user info on orig user")		
	}
	orig_user_infos := origUserResponse.GetUsers() // type []*UserInfo
	if len(orig_user_infos) != 1 {
		// This should never happen
		if !server.options.DisableFallback {
			// Use fallback
			cachedVideos, _ := GetCachedTrendingVideos(server)
			if cachedVideos != nil {
				defer updateStats(server, startTime, false, false, false, true)
				return &pb.GetTopVideosResponse{Videos: cachedVideos}, nil
			}
		}
		defer updateStats(server, startTime, true, false, false, false)
		return nil, handleError(nil, fmt.Sprintf("incorrect number (%d) of UserInfos for orig user", len(orig_user_infos)))
	}
	orig_user_info := orig_user_infos[0]

	// (3) Fetch users that the orig user subscribes to
	subscribed_to := orig_user_info.GetSubscribedTo()

	// (4) Fetch the liked videos of the subscribe users
	subscribed_user_infos := make([]*upb.UserInfo, 0)

	// Batching:
	batchSize := server.options.MaxBatchSize
	if batchSize > 0 {
		// Batching
		for len(subscribed_to) > 0 {
			// Slice for next request
			sub := make([]uint64, 0)

			if len(subscribed_to) > batchSize {
				sub = subscribed_to[:batchSize]
				subscribed_to = subscribed_to[batchSize:]
			} else {
				sub = subscribed_to
				subscribed_to = make([]uint64, 0)
			}

			likedVideoResponse, err := userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: sub})
			if err != nil {
				// Retry
				if server.options.DisableRetry {
					likedVideoResponse, err = userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: sub})
					if err != nil {
						if !server.options.DisableFallback {
							// Use fallback
							cachedVideos, _ := GetCachedTrendingVideos(server)
							if cachedVideos != nil {
								defer updateStats(server, startTime, false, false, false, true)
								return &pb.GetTopVideosResponse{Videos: cachedVideos}, nil
							}
						}
					}
				}

				defer updateStats(server, startTime, true, true, false, false)
				return nil, handleError(err, "fail to fetch liked videos in batch")				
			}

			subscribed_user_infos = append(subscribed_user_infos, likedVideoResponse.GetUsers()...)
		}
	} else {
		// No batching
		for _, s := range subscribed_to {
			likedVideoResponse, err := userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: []uint64{s}})
			if err != nil {
				// Retry
				if server.options.DisableRetry {
					likedVideoResponse, err = userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: []uint64{s}})
					if err != nil {
						if !server.options.DisableFallback {
							// Use fallback
							cachedVideos, _ := GetCachedTrendingVideos(server)
							if cachedVideos != nil {
								defer updateStats(server, startTime, false, false, false, true)
								return &pb.GetTopVideosResponse{Videos: cachedVideos}, nil
							}
						}
					}
				}
				
				defer updateStats(server, startTime, true, true, false, false)
				return nil, handleError(err, "fail to fetch liked videos")
			}
	
			subscribed_user_infos = append(subscribed_user_infos, likedVideoResponse.GetUsers()...)
		}
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

	// (1) Create VideoService client
	var videoClient vpb.VideoServiceClient
	if server.useMock {
		videoClient = server.mockVideoServiceClient
	} else {
		// Create gRPC channel for VideoService
		var optsVideo []grpc.DialOption
		optsVideo = append(optsVideo, grpc.WithTransportCredentials(insecure.NewCredentials()))

		connVideo, err := grpc.Dial(server.options.VideoServiceAddr, optsVideo...)
		if err != nil {
			// Retry
			if server.options.DisableRetry {
				connVideo, err = grpc.Dial(server.options.VideoServiceAddr, optsVideo...)
				if err != nil {
					if !server.options.DisableFallback {
						// Use fallback
						cachedVideos, _ := GetCachedTrendingVideos(server)
						if cachedVideos != nil {
							defer updateStats(server, startTime, false, false, false, true)
							return &pb.GetTopVideosResponse{Videos: cachedVideos}, nil
						}
					}
				}
			}
			
			defer updateStats(server, startTime, true, false, true, false)
			return nil, handleError(err, "fail to dial")
		}
		defer connVideo.Close()

		// Create VideoService client stub
		videoClient = vpb.NewVideoServiceClient(connVideo)
	}
	
	// (2) Fetch video infos for liked videos
	video_infos := make([]*vpb.VideoInfo, 0)

	// Batching:
	if batchSize > 0 {
		// Batching
		for len(liked_videos) > 0 {
			// Slice for next request
			vids := make([]uint64, 0)

			if len(liked_videos) > batchSize {
				vids = liked_videos[:batchSize]
				liked_videos = liked_videos[batchSize:]
			} else {
				vids = liked_videos
				liked_videos = make([]uint64, 0)
			}

			videoResponse, err := videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: vids})
			if err != nil {
				// Retry
				if server.options.DisableRetry {
					videoResponse, err = videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: vids})
					if err != nil {
						if !server.options.DisableFallback {
							// Use fallback
							cachedVideos, _ := GetCachedTrendingVideos(server)
							if cachedVideos != nil {
								defer updateStats(server, startTime, false, false, false, true)
								return &pb.GetTopVideosResponse{Videos: cachedVideos}, nil
							}
						}
					}
				}
				
				defer updateStats(server, startTime, true, false, true, false)
				return nil, handleError(err, "fail to fetch video infos")
			}
	
			video_infos = append(video_infos, videoResponse.GetVideos()...)
		}
	} else {
		// No batching
		for _, v := range liked_videos {
			videoResponse, err := videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: []uint64{v}})
			if err != nil {
				// Retry
				if server.options.DisableRetry {
					videoResponse, err = videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: []uint64{v}})
					if err != nil {
						if !server.options.DisableFallback {
							// Use fallback
							cachedVideos, _ := GetCachedTrendingVideos(server)
							if cachedVideos != nil {
								defer updateStats(server, startTime, false, false, false, true)
								return &pb.GetTopVideosResponse{Videos: cachedVideos}, nil
							}
						}
					}
				}
				
				defer updateStats(server, startTime, true, false, true, false)
				return nil, handleError(err, "fail to fetch video infos")
			}
	
			video_infos = append(video_infos, videoResponse.GetVideos()...)
		}
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

	defer updateStats(server, startTime, false, false, false, false)
	return &pb.GetTopVideosResponse{Videos: liked_videos_ranked}, nil
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