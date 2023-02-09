package main

import (
	"context"
	"testing"
	"time"
	"log"

	"github.com/stretchr/testify/assert"

	fipb "cs426.yale.edu/lab1/failure_injection/proto"
	umc "cs426.yale.edu/lab1/user_service/mock_client"
	usl "cs426.yale.edu/lab1/user_service/server_lib"
	vmc "cs426.yale.edu/lab1/video_service/mock_client"
	vsl "cs426.yale.edu/lab1/video_service/server_lib"

	pb "cs426.yale.edu/lab1/video_rec_service/proto"
	sl "cs426.yale.edu/lab1/video_rec_service/server_lib"
)

func TestServerBasic(t *testing.T) {
	vrOptions := sl.VideoRecServiceOptions{
		MaxBatchSize:    50,
		DisableFallback: true,
		DisableRetry:    true,
	}
	// You can specify failure injection options here or later send
	// SetInjectionConfigRequests using these mock clients
	uClient :=
		umc.MakeMockUserServiceClient(*usl.DefaultUserServiceOptions())
	vClient :=
		vmc.MakeMockVideoServiceClient(*vsl.DefaultVideoServiceOptions())
	vrService := sl.MakeVideoRecServiceServerWithMocks(
		vrOptions,
		uClient,
		vClient,
	)

	var userId uint64 = 204054
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := vrService.GetTopVideos(
		ctx,
		&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
	)
	assert.True(t, err == nil)

	videos := out.Videos
	assert.Equal(t, 5, len(videos))
	assert.EqualValues(t, 1012, videos[0].VideoId)
	assert.Equal(t, "Harry Boehm", videos[1].Author)
	assert.EqualValues(t, 1209, videos[2].VideoId)
	assert.Equal(t, "https://video-data.localhost/blob/1309", videos[3].Url)
	assert.Equal(t, "precious here", videos[4].Title)

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	vClient.SetInjectionConfig(ctx, &fipb.SetInjectionConfigRequest{
		Config: &fipb.InjectionConfig{
			// fail one in 1 request, i.e., always fail
			FailureRate: 1,
		},
	})

	// Since we disabled retry and fallback, we expect the VideoRecService to
	// throw an error since the VideoService is "down".
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = vrService.GetTopVideos(
		ctx,
		&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
	)
	assert.False(t, err == nil)
}

func TestFetchUserInfos(t *testing.T){
	vrOptions := sl.VideoRecServiceOptions{MaxBatchSize: 50}
	uClient :=
		umc.MakeMockUserServiceClient(*usl.DefaultUserServiceOptions())
	vClient :=
		vmc.MakeMockVideoServiceClient(*vsl.DefaultVideoServiceOptions())
	vrService := sl.MakeVideoRecServiceServerWithMocks(
		vrOptions,
		uClient,
		vClient,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := sl.FetchUserInfos(ctx, vrService, uClient, []uint64{202549})
	assert.True(t, err == nil)

	assert.Equal(t, 1, len(out))
	assert.Equal(t, "Abbott4742", out[0].Username)
	assert.Equal(t, "averymills@bechtelar.io", out[0].Email)
	assert.Equal(t, "https://user-service.localhost/profile/202549", out[0].ProfileUrl)
}

// func TestFetchVideoInfos(t *testing.T){
// 	vrOptions := sl.VideoRecServiceOptions{MaxBatchSize: 50}
// 	uClient :=
// 		umc.MakeMockUserServiceClient(*usl.DefaultUserServiceOptions())
// 	vClient :=
// 		vmc.MakeMockVideoServiceClient(*vsl.DefaultVideoServiceOptions())
// 	vrService := sl.MakeVideoRecServiceServerWithMocks(
// 		vrOptions,
// 		uClient,
// 		vClient,
// 	)

// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()

// 	out, err := sl.FetchVideoInfos(ctx, vrService, vClient, []uint64{1386})
// 	assert.True(t, err == nil)

// 	assert.Equal(t, 1, len(out))
// 	assert.EqualValues(t, 1386, out[0].VideoId)
// 	assert.Equal(t, "ugly Corn", out[0].Title)
// 	assert.Equal(t, "Adolfo Runolfsdottir", out[0].Author)
// 	assert.Equal(t, "https://video-data.localhost/blob/1386", out[0].Url)
// }

func TestReqLimit(t *testing.T){
	vrOptions := sl.VideoRecServiceOptions{MaxBatchSize: 50}
	uClient :=
		umc.MakeMockUserServiceClient(*usl.DefaultUserServiceOptions())
	vClient :=
		vmc.MakeMockVideoServiceClient(*vsl.DefaultVideoServiceOptions())
	vrService := sl.MakeVideoRecServiceServerWithMocks(
		vrOptions,
		uClient,
		vClient,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var userId uint64 = 204054

	out, err := vrService.GetTopVideos(
		ctx,
		&pb.GetTopVideosRequest{UserId: userId, Limit: 0},
	)
	assert.True(t, err == nil)
	log.Println(out)
	numVideos := len(out.Videos)
	
	var limit int32;
	for limit = 5; limit < 50; limit += 5 {
		out, err := vrService.GetTopVideos(
			ctx,
			&pb.GetTopVideosRequest{UserId: userId, Limit: limit},
		)
		assert.True(t, err == nil)

		videos := out.Videos
		if numVideos < int(limit) {
			assert.True(t, len(videos) == numVideos)
		} else {
			assert.True(t, len(videos) == int(limit))
		}
	}
}

// func TestFallback(t *testing.T){
// 	vrOptions := sl.VideoRecServiceOptions{
// 		MaxBatchSize:    50,
// 		DisableFallback: false,
// 		DisableRetry:    true,
// 	}
// 	uClient :=
// 		umc.MakeMockUserServiceClient(*usl.DefaultUserServiceOptions())
// 	vClient :=
// 		vmc.MakeMockVideoServiceClient(*vsl.DefaultVideoServiceOptions())
// 	vrService := sl.MakeVideoRecServiceServerWithMocks(
// 		vrOptions,
// 		uClient,
// 		vClient,
// 	)

// 	// 10 without failure so that updateTrendingVideos has time to initialize
// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()
// 	var userId uint64 = 204054
// 	for i := 0; i < 10; i++ {
// 		_, err := vrService.GetTopVideos(
// 			ctx,
// 			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
// 		)
// 		assert.True(t, err == nil)
// 	}

// 	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()
// 	vClient.SetInjectionConfig(ctx, &fipb.SetInjectionConfigRequest{
// 		Config: &fipb.InjectionConfig{
// 			// fail every request
// 			FailureRate: 1,
// 		},
// 	})

// 	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()
// 	out, err := vrService.GetTopVideos(
// 		ctx,
// 		&pb.GetTopVideosRequest{UserId: userId, Limit: 7},
// 	)
// 	assert.True(t, err == nil)

// 	// Should be fetching trendingVideos
// 	videos := out.Videos
// 	assert.EqualValues(t, 7, len(videos))
// 	assert.EqualValues(t, 1316, videos[0].VideoId)
// 	assert.Equal(t, "Goatknit: index", videos[1].Title)
// 	assert.Equal(t, "Howell Hills", videos[2].Author)
// 	assert.Equal(t, "https://video-data.localhost/blob/1132", videos[3].Url)
// 	assert.EqualValues(t, 1016, videos[4].VideoId)
// 	assert.Equal(t, "Juicerbeing: connect", videos[5].Title)
// 	assert.Equal(t, "Electa Kris", videos[6].Author)
// }

// func TestBatchSize(t *testing.T){
// 	vrOptions := sl.VideoRecServiceOptions{
// 		MaxBatchSize:    50,
// 		DisableFallback: true,
// 		DisableRetry:    true,
// 	}
// 	uOptions := usl.UserServiceOptions{MaxBatchSize: 10}
// 	vOptions := vsl.VideoServiceOptions{MaxBatchSize: 10}

// 	uClient := umc.MakeMockUserServiceClient(uOptions)
// 	vClient := vmc.MakeMockVideoServiceClient(vOptions)

// 	vrService := sl.MakeVideoRecServiceServerWithMocks(
// 		vrOptions,
// 		uClient,
// 		vClient,
// 	)

// 	var userId uint64 = 204054
// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()

// 	_, err := vrService.GetTopVideos(
// 		ctx,
// 		&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
// 	)
// 	assert.True(t, err != nil) // expect failure
// }

// func TestRetry(t *testing.T){
// 	uClient := umc.MakeMockUserServiceClient(*usl.DefaultUserServiceOptions())
// 	vClient := vmc.MakeMockVideoServiceClient(*vsl.DefaultVideoServiceOptions())
	
// 	// Inject failure to vClient
// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()
// 	vClient.SetInjectionConfig(ctx, &fipb.SetInjectionConfigRequest{
// 		Config: &fipb.InjectionConfig{
// 			FailureRate: 10,
// 		},
// 	})

// 	// vrService WITHOUT retry
// 	vrOptions := sl.VideoRecServiceOptions{
// 		MaxBatchSize:    50,
// 		DisableFallback: true,
// 		DisableRetry:    true,
// 	}
// 	vrService := sl.MakeVideoRecServiceServerWithMocks(
// 		vrOptions,
// 		uClient,
// 		vClient,
// 	)

// 	// 100 runs without retry
// 	var userId uint64 = 204054
// 	for i := 0; i < 50; i++ {
// 		vrService.GetTopVideos(
// 			ctx,
// 			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
// 		)
// 	}

// 	// Get stats
// 	statsResponse, err := vrService.GetStats(ctx, &pb.GetStatsRequest{})
// 	assert.True(t, err == nil)
// 	numErrorWithoutRetry := statsResponse.GetTotalErrors()

// 	// vrService WITH retry
// 	vrOptions = sl.VideoRecServiceOptions{
// 		MaxBatchSize:    50,
// 		DisableFallback: true,
// 		DisableRetry:    false,
// 	}
// 	vrService = sl.MakeVideoRecServiceServerWithMocks(
// 		vrOptions,
// 		uClient,
// 		vClient,
// 	)

// 	// 100 runs with retry
// 	for i := 0; i < 50; i++ {
// 		vrService.GetTopVideos(
// 			ctx,
// 			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
// 		)
// 	}

// 	// Get stats
// 	statsResponse, err = vrService.GetStats(ctx, &pb.GetStatsRequest{})
// 	assert.True(t, err == nil)
// 	numErrorWithRetry := statsResponse.GetTotalErrors()

// 	assert.True(t, numErrorWithRetry <= numErrorWithoutRetry)
// }

// func TestStatsNoFailure(t *testing.T) {
// 	vrOptions := sl.VideoRecServiceOptions{MaxBatchSize: 50}
// 	uClient :=
// 		umc.MakeMockUserServiceClient(*usl.DefaultUserServiceOptions())
// 	vClient :=
// 		vmc.MakeMockVideoServiceClient(*vsl.DefaultVideoServiceOptions())
// 	vrService := sl.MakeVideoRecServiceServerWithMocks(
// 		vrOptions,
// 		uClient,
// 		vClient,
// 	)

// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()

// 	var userId uint64 = 204054
// 	for i := 0; i < 10; i++ {
// 		out, err := vrService.GetTopVideos(
// 			ctx,
// 			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
// 		)
// 		assert.True(t, err == nil)

// 		if i == 0 {
// 			videos := out.Videos
// 			assert.Equal(t, 5, len(videos))
// 			assert.EqualValues(t, 1012, videos[0].VideoId)
// 			assert.Equal(t, "Harry Boehm", videos[1].Author)
// 			assert.EqualValues(t, 1209, videos[2].VideoId)
// 			assert.Equal(t, "https://video-data.localhost/blob/1309", videos[3].Url)
// 			assert.Equal(t, "precious here", videos[4].Title)
// 		}
// 	}

// 	// Get stats
// 	statsResponse, err := vrService.GetStats(ctx, &pb.GetStatsRequest{})
// 	assert.True(t, err == nil)
// 	assert.Equal(t, uint64(10), statsResponse.GetTotalRequests())
// 	assert.Equal(t, uint64(0), statsResponse.GetTotalErrors())
// 	assert.Equal(t, uint64(0), statsResponse.GetActiveRequests())
// 	assert.Equal(t, uint64(0), statsResponse.GetUserServiceErrors())
// 	assert.Equal(t, uint64(0), statsResponse.GetVideoServiceErrors())
// 	assert.Equal(t, uint64(0), statsResponse.GetStaleResponses())
// }

// func TestStatsWithFailure(t *testing.T) {
// 	vrOptions := sl.VideoRecServiceOptions{
// 		MaxBatchSize:    50,
// 		DisableFallback: true,
// 		DisableRetry:    true,
// 	}
// 	uClient :=
// 		umc.MakeMockUserServiceClient(*usl.DefaultUserServiceOptions())
// 	vClient :=
// 		vmc.MakeMockVideoServiceClient(*vsl.DefaultVideoServiceOptions())
// 	vrService := sl.MakeVideoRecServiceServerWithMocks(
// 		vrOptions,
// 		uClient,
// 		vClient,
// 	)

// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()
// 	vClient.SetInjectionConfig(ctx, &fipb.SetInjectionConfigRequest{
// 		Config: &fipb.InjectionConfig{
// 			// fail every request
// 			FailureRate: 1,
// 		},
// 	})

// 	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()

// 	var userId uint64 = 204054
// 	for i := 0; i < 10; i++ {
// 		_, err := vrService.GetTopVideos(
// 			ctx,
// 			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
// 		)
// 		assert.False(t, err == nil)
// 	}

// 	// Get stats
// 	statsResponse, err := vrService.GetStats(ctx, &pb.GetStatsRequest{})
// 	assert.True(t, err == nil)
// 	assert.Equal(t, uint64(10), statsResponse.GetTotalRequests())
// 	assert.Equal(t, uint64(10), statsResponse.GetTotalErrors())
// 	assert.Equal(t, uint64(0), statsResponse.GetActiveRequests())
// 	assert.Equal(t, uint64(0), statsResponse.GetUserServiceErrors())
// 	assert.Equal(t, uint64(10), statsResponse.GetVideoServiceErrors())
// 	assert.Equal(t, uint64(0), statsResponse.GetStaleResponses())
// }

// func TestStatsWithFallback(t *testing.T) {
// 	vrOptions := sl.VideoRecServiceOptions{
// 		MaxBatchSize:    50,
// 		DisableFallback: false,
// 		DisableRetry:    true,
// 	}
// 	uClient :=
// 		umc.MakeMockUserServiceClient(*usl.DefaultUserServiceOptions())
// 	vClient :=
// 		vmc.MakeMockVideoServiceClient(*vsl.DefaultVideoServiceOptions())
// 	vrService := sl.MakeVideoRecServiceServerWithMocks(
// 		vrOptions,
// 		uClient,
// 		vClient,
// 	)

// 	// 10 without failure
// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()
// 	var userId uint64 = 204054
// 	for i := 0; i < 10; i++ {
// 		_, err := vrService.GetTopVideos(
// 			ctx,
// 			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
// 		)
// 		assert.True(t, err == nil)
// 	}

// 	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()
// 	uClient.SetInjectionConfig(ctx, &fipb.SetInjectionConfigRequest{
// 		Config: &fipb.InjectionConfig{
// 			// fail every request
// 			FailureRate: 1,
// 		},
// 	})

// 	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()

// 	// 10 with failure + fallback
// 	for i := 0; i < 10; i++ {
// 		_, err := vrService.GetTopVideos(
// 			ctx,
// 			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
// 		)
// 		assert.True(t, err == nil)
// 	}

// 	// Get stats
// 	statsResponse, err := vrService.GetStats(ctx, &pb.GetStatsRequest{})
// 	assert.True(t, err == nil)
// 	assert.Equal(t, uint64(20), statsResponse.GetTotalRequests())
// 	assert.Equal(t, uint64(0), statsResponse.GetTotalErrors())
// 	assert.Equal(t, uint64(0), statsResponse.GetActiveRequests())
// 	assert.Equal(t, uint64(0), statsResponse.GetUserServiceErrors())
// 	assert.Equal(t, uint64(0), statsResponse.GetVideoServiceErrors())
// 	assert.Equal(t, uint64(10), statsResponse.GetStaleResponses())
// }