package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	models "github.com/drama-generator/backend/domain/models"
	"github.com/drama-generator/backend/infrastructure/external/ffmpeg"
	"github.com/drama-generator/backend/infrastructure/storage"
	"github.com/drama-generator/backend/pkg/logger"
	"github.com/drama-generator/backend/pkg/utils"
	"github.com/drama-generator/backend/pkg/video"
	"gorm.io/gorm"
)

type VideoGenerationService struct {
	db              *gorm.DB
	transferService *ResourceTransferService
	log             *logger.Logger
	localStorage    *storage.LocalStorage
	aiService       *AIService
	ffmpeg          *ffmpeg.FFmpeg
	promptI18n      *PromptI18n
	taskService     *TaskService
}

func NewVideoGenerationService(db *gorm.DB, transferService *ResourceTransferService, localStorage *storage.LocalStorage, aiService *AIService, log *logger.Logger, promptI18n *PromptI18n) *VideoGenerationService {
	service := &VideoGenerationService{
		db:              db,
		localStorage:    localStorage,
		transferService: transferService,
		aiService:       aiService,
		log:             log,
		ffmpeg:          ffmpeg.NewFFmpeg(log),
		promptI18n:      promptI18n,
		taskService:     NewTaskService(db, log),
	}

	go service.RecoverPendingTasks()

	return service
}

type GenerateVideoRequest struct {
	StoryboardID *uint  `json:"storyboard_id"`
	DramaID      string `json:"drama_id" binding:"required"`
	ImageGenID   *uint  `json:"image_gen_id"`

	// 参考图模式：single, first_last, multiple, none
	ReferenceMode string `json:"reference_mode"`

	// 单图模式
	ImageURL       string  `json:"image_url"`
	ImageLocalPath *string `json:"image_local_path"` // 单图模式的本地路径

	// 首尾帧模式
	FirstFrameURL       *string `json:"first_frame_url"`
	FirstFrameLocalPath *string `json:"first_frame_local_path"` // 首帧本地路径
	LastFrameURL        *string `json:"last_frame_url"`
	LastFrameLocalPath  *string `json:"last_frame_local_path"` // 尾帧本地路径

	// 多图模式
	ReferenceImageURLs []string `json:"reference_image_urls"`

	Prompt       string  `json:"prompt" binding:"required,min=5,max=2000"`
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	Duration     *int    `json:"duration"`
	FPS          *int    `json:"fps"`
	AspectRatio  *string `json:"aspect_ratio"`
	Style        *string `json:"style"`
	MotionLevel  *int    `json:"motion_level"`
	CameraMotion *string `json:"camera_motion"`
	Seed         *int64  `json:"seed"`
}

func (s *VideoGenerationService) GenerateVideo(request *GenerateVideoRequest) (*models.VideoGeneration, error) {
	if request.StoryboardID != nil {
		var storyboard models.Storyboard
		if err := s.db.Preload("Episode").Where("id = ?", *request.StoryboardID).First(&storyboard).Error; err != nil {
			return nil, fmt.Errorf("storyboard not found")
		}
		if fmt.Sprintf("%d", storyboard.Episode.DramaID) != request.DramaID {
			return nil, fmt.Errorf("storyboard does not belong to drama")
		}
	}

	if request.ImageGenID != nil {
		var imageGen models.ImageGeneration
		if err := s.db.Where("id = ?", *request.ImageGenID).First(&imageGen).Error; err != nil {
			return nil, fmt.Errorf("image generation not found")
		}
	}

	provider := request.Provider
	if provider == "" {
		provider = "doubao"
	}

	dramaID, _ := strconv.ParseUint(request.DramaID, 10, 32)

	videoGen := &models.VideoGeneration{
		StoryboardID: request.StoryboardID,
		DramaID:      uint(dramaID),
		ImageGenID:   request.ImageGenID,
		Provider:     provider,
		Prompt:       request.Prompt,
		Model:        request.Model,
		Duration:     request.Duration,
		FPS:          request.FPS,
		AspectRatio:  request.AspectRatio,
		Style:        request.Style,
		MotionLevel:  request.MotionLevel,
		CameraMotion: request.CameraMotion,
		Seed:         request.Seed,
		Status:       models.VideoStatusPending,
	}

	// 根据参考图模式处理不同的参数
	if request.ReferenceMode != "" {
		videoGen.ReferenceMode = &request.ReferenceMode
	}

	switch request.ReferenceMode {
	case "single":
		// 单图模式 - 优先使用 local_path
		if request.ImageLocalPath != nil && *request.ImageLocalPath != "" {
			videoGen.ImageURL = request.ImageLocalPath
		} else if request.ImageURL != "" {
			videoGen.ImageURL = &request.ImageURL
		}
	case "first_last":
		// 首尾帧模式 - 优先使用 local_path
		if request.FirstFrameLocalPath != nil && *request.FirstFrameLocalPath != "" {
			videoGen.FirstFrameURL = request.FirstFrameLocalPath
		} else if request.FirstFrameURL != nil {
			videoGen.FirstFrameURL = request.FirstFrameURL
		}
		if request.LastFrameLocalPath != nil && *request.LastFrameLocalPath != "" {
			videoGen.LastFrameURL = request.LastFrameLocalPath
		} else if request.LastFrameURL != nil {
			videoGen.LastFrameURL = request.LastFrameURL
		}
	case "multiple":
		// 多图模式
		if len(request.ReferenceImageURLs) > 0 {
			referenceImagesJSON, err := json.Marshal(request.ReferenceImageURLs)
			if err == nil {
				referenceImagesStr := string(referenceImagesJSON)
				videoGen.ReferenceImageURLs = &referenceImagesStr
			}
		}
	case "none":
		// 无参考图，纯文本生成
	default:
		// 向后兼容：如果没有指定模式，根据提供的参数自动判断
		if request.ImageURL != "" {
			videoGen.ImageURL = &request.ImageURL
			mode := "single"
			videoGen.ReferenceMode = &mode
		} else if request.FirstFrameURL != nil || request.LastFrameURL != nil {
			videoGen.FirstFrameURL = request.FirstFrameURL
			videoGen.LastFrameURL = request.LastFrameURL
			mode := "first_last"
			videoGen.ReferenceMode = &mode
		} else if len(request.ReferenceImageURLs) > 0 {
			referenceImagesJSON, err := json.Marshal(request.ReferenceImageURLs)
			if err == nil {
				referenceImagesStr := string(referenceImagesJSON)
				videoGen.ReferenceImageURLs = &referenceImagesStr
				mode := "multiple"
				videoGen.ReferenceMode = &mode
			}
		}
	}

	if err := s.db.Create(videoGen).Error; err != nil {
		return nil, fmt.Errorf("failed to create record: %w", err)
	}

	// Start background goroutine to process video generation asynchronously
	// This allows the API to return immediately while video generation happens in background
	// CRITICAL: The goroutine will handle all video generation logic including API calls and polling
	go s.ProcessVideoGeneration(videoGen.ID)

	return videoGen, nil
}

func (s *VideoGenerationService) ProcessVideoGeneration(videoGenID uint) {
	var videoGen models.VideoGeneration
	if err := s.db.First(&videoGen, videoGenID).Error; err != nil {
		s.log.Errorw("Failed to load video generation", "error", err, "id", videoGenID)
		return
	}

	// 获取drama的style信息
	var drama models.Drama
	if err := s.db.First(&drama, videoGen.DramaID).Error; err != nil {
		s.log.Warnw("Failed to load drama for style", "error", err, "drama_id", videoGen.DramaID)
	}

	s.db.Model(&videoGen).Update("status", models.VideoStatusProcessing)

	client, err := s.getVideoClient(videoGen.Provider, videoGen.Model)
	if err != nil {
		s.log.Errorw("Failed to get video client", "error", err, "provider", videoGen.Provider, "model", videoGen.Model)
		s.updateVideoGenError(videoGenID, err.Error())
		return
	}

	s.log.Infow("Starting video generation", "id", videoGenID, "prompt", videoGen.Prompt, "provider", videoGen.Provider)

	var opts []video.VideoOption
	if videoGen.Model != "" {
		opts = append(opts, video.WithModel(videoGen.Model))
	}
	if videoGen.Duration != nil {
		opts = append(opts, video.WithDuration(*videoGen.Duration))
	}
	if videoGen.FPS != nil {
		opts = append(opts, video.WithFPS(*videoGen.FPS))
	}
	if videoGen.AspectRatio != nil {
		opts = append(opts, video.WithAspectRatio(*videoGen.AspectRatio))
	}
	if videoGen.Style != nil {
		opts = append(opts, video.WithStyle(*videoGen.Style))
	}
	if videoGen.MotionLevel != nil {
		opts = append(opts, video.WithMotionLevel(*videoGen.MotionLevel))
	}
	if videoGen.CameraMotion != nil {
		opts = append(opts, video.WithCameraMotion(*videoGen.CameraMotion))
	}
	if videoGen.Seed != nil {
		opts = append(opts, video.WithSeed(*videoGen.Seed))
	}
	// 使用drama的视频分辨率设置
	if drama.VideoResolution != "" {
		opts = append(opts, video.WithResolution(drama.VideoResolution))
		s.log.Infow("Using drama video resolution", "id", videoGenID, "resolution", drama.VideoResolution)
	}

	// 根据参考图模式添加相应的选项，并将本地图片转换为base64
	if videoGen.ReferenceMode != nil {
		switch *videoGen.ReferenceMode {
		case "first_last":
			// 首尾帧模式 - 转换本地图片为base64
			if videoGen.FirstFrameURL != nil {
				firstFrameBase64, err := s.convertImageToBase64(*videoGen.FirstFrameURL)
				if err != nil {
					s.log.Warnw("Failed to convert first frame to base64, using original URL", "error", err)
					opts = append(opts, video.WithFirstFrame(*videoGen.FirstFrameURL))
				} else {
					opts = append(opts, video.WithFirstFrame(firstFrameBase64))
				}
			}
			if videoGen.LastFrameURL != nil {
				lastFrameBase64, err := s.convertImageToBase64(*videoGen.LastFrameURL)
				if err != nil {
					s.log.Warnw("Failed to convert last frame to base64, using original URL", "error", err)
					opts = append(opts, video.WithLastFrame(*videoGen.LastFrameURL))
				} else {
					opts = append(opts, video.WithLastFrame(lastFrameBase64))
				}
			}
		case "multiple":
			// 多图模式 - 转换本地图片为base64
			if videoGen.ReferenceImageURLs != nil {
				var imageURLs []string
				if err := json.Unmarshal([]byte(*videoGen.ReferenceImageURLs), &imageURLs); err == nil {
					var base64Images []string
					for _, imgURL := range imageURLs {
						base64Img, err := s.convertImageToBase64(imgURL)
						if err != nil {
							s.log.Warnw("Failed to convert reference image to base64, using original URL", "error", err, "url", imgURL)
							base64Images = append(base64Images, imgURL)
						} else {
							base64Images = append(base64Images, base64Img)
						}
					}
					opts = append(opts, video.WithReferenceImages(base64Images))
				}
			}
		}
	}

	// 构造imageURL参数（单图模式使用，其他模式传空字符串）
	// 如果是本地图片，转换为base64
	imageURL := ""
	if videoGen.ImageURL != nil {
		base64Image, err := s.convertImageToBase64(*videoGen.ImageURL)
		if err != nil {
			s.log.Warnw("Failed to convert image to base64, using original URL", "error", err)
			imageURL = *videoGen.ImageURL
		} else {
			imageURL = base64Image
		}
	}

	// 构建完整的提示词：风格提示词 + 约束提示词 + 用户提示词
	prompt := videoGen.Prompt

	// 2. 添加视频约束提示词
	// 根据参考图模式选择对应的约束提示词
	referenceMode := "single" // 默认单图模式
	if videoGen.ReferenceMode != nil {
		referenceMode = *videoGen.ReferenceMode
	}

	// 如果是单图模式，需要检查图片是否为动作序列图
	if referenceMode == "single" && videoGen.ImageGenID != nil {
		var imageGen models.ImageGeneration
		if err := s.db.First(&imageGen, *videoGen.ImageGenID).Error; err == nil {
			// 如果图片的frame_type是action，使用动作序列约束提示词
			if imageGen.FrameType != nil && *imageGen.FrameType == "action" {
				referenceMode = "action_sequence"
				s.log.Infow("Detected action sequence image in single mode",
					"id", videoGenID,
					"image_gen_id", *videoGen.ImageGenID,
					"frame_type", *imageGen.FrameType)
			}
		}
	}

	constraintPrompt := s.promptI18n.GetVideoConstraintPrompt(referenceMode)
	if constraintPrompt != "" {
		prompt = constraintPrompt + "\n\n" + prompt
		s.log.Infow("Added constraint prompt to video generation",
			"id", videoGenID,
			"reference_mode", referenceMode,
			"constraint_prompt_length", len(constraintPrompt))
	}

	// 打印完整的提示词信息
	s.log.Infow("Video generation prompts",
		"id", videoGenID,
		"user_prompt", videoGen.Prompt,
		"constraint_prompt", constraintPrompt,
		"final_prompt", prompt)

	result, err := client.GenerateVideo(imageURL, prompt, opts...)
	if err != nil {
		s.log.Errorw("Video generation API call failed", "error", err, "id", videoGenID)
		s.updateVideoGenError(videoGenID, err.Error())
		return
	}

	// CRITICAL FIX: Validate TaskID before starting polling goroutine
	// Empty TaskID would cause polling to fail silently or cause issues
	if result.TaskID != "" {
		s.db.Model(&videoGen).Updates(map[string]interface{}{
			"task_id": result.TaskID,
			"status":  models.VideoStatusProcessing,
		})
		// Start background goroutine to poll task status
		// This allows the API to return immediately while video generation continues asynchronously
		// The goroutine will poll until completion, failure, or timeout (max 300 attempts * 10s = 50 minutes)
		go s.pollTaskStatus(videoGenID, result.TaskID, videoGen.Provider, videoGen.Model)
		return
	}

	if result.VideoURL != "" {
		s.completeVideoGeneration(videoGenID, result.VideoURL, &result.Duration, &result.Width, &result.Height, nil)
		return
	}

	s.updateVideoGenError(videoGenID, "no task ID or video URL returned")
}

func (s *VideoGenerationService) pollTaskStatus(videoGenID uint, taskID string, provider string, model string) {
	// CRITICAL FIX: Validate taskID parameter to prevent invalid API calls
	// Empty taskID would cause unnecessary API calls and potential errors
	if taskID == "" {
		s.log.Errorw("Invalid empty taskID for polling", "video_gen_id", videoGenID)
		s.updateVideoGenError(videoGenID, "invalid task ID for polling")
		return
	}

	client, err := s.getVideoClient(provider, model)
	if err != nil {
		s.log.Errorw("Failed to get video client for polling", "error", err)
		s.updateVideoGenError(videoGenID, "failed to get video client")
		return
	}

	// Polling configuration: max 300 attempts with 10 second intervals
	// Total maximum polling time: 300 * 10s = 50 minutes
	// This prevents infinite polling if the task never completes
	maxAttempts := 300
	interval := 10 * time.Second

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Sleep before each poll attempt to avoid overwhelming the API
		// First iteration sleeps before the first check (after 0 attempts)
		time.Sleep(interval)

		var videoGen models.VideoGeneration
		if err := s.db.First(&videoGen, videoGenID).Error; err != nil {
			s.log.Errorw("Failed to load video generation", "error", err, "id", videoGenID)
			return
		}

		// CRITICAL FIX: Check if status was manually changed (e.g., cancelled by user)
		// If status is no longer "processing", stop polling to avoid unnecessary API calls
		// This prevents polling when the task has been cancelled or failed externally
		if videoGen.Status != models.VideoStatusProcessing {
			s.log.Infow("Video generation status changed, stopping poll", "id", videoGenID, "status", videoGen.Status)
			return
		}

		// Poll the video generation API for task status
		// Continue polling on transient errors (network issues, temporary API failures)
		// Only stop on permanent errors or task completion
		result, err := client.GetTaskStatus(taskID)
		if err != nil {
			s.log.Errorw("Failed to get task status", "error", err, "task_id", taskID, "attempt", attempt+1)
			// Continue polling on error - might be transient network issue
			// Will eventually timeout after maxAttempts if error persists
			continue
		}

		// Check if task completed successfully
		// CRITICAL FIX: Validate that video URL exists when task is marked as completed
		// Some APIs may mark task as completed but fail to provide the video URL
		if result.Completed {
			if result.VideoURL != "" {
				// Successfully completed with video URL - download and update database
				s.completeVideoGeneration(videoGenID, result.VideoURL, &result.Duration, &result.Width, &result.Height, nil)
				return
			}
			// Task marked as completed but no video URL - this is an error condition
			s.updateVideoGenError(videoGenID, "task completed but no video URL")
			return
		}

		// Check if task failed with an error message
		if result.Error != "" {
			s.updateVideoGenError(videoGenID, result.Error)
			return
		}

		// Task still in progress - log and continue polling
		s.log.Infow("Video generation in progress", "id", videoGenID, "attempt", attempt+1, "max_attempts", maxAttempts)
	}

	// CRITICAL FIX: Handle polling timeout gracefully
	// After maxAttempts (50 minutes), mark task as failed if still not completed
	// This prevents indefinite polling and resource waste
	s.updateVideoGenError(videoGenID, fmt.Sprintf("polling timeout after %d attempts (%.1f minutes)", maxAttempts, float64(maxAttempts*int(interval))/60.0))
}

func (s *VideoGenerationService) completeVideoGeneration(videoGenID uint, videoURL string, duration *int, width *int, height *int, firstFrameURL *string) {
	var localVideoPath *string

	// 下载视频到本地存储并保存相对路径到数据库
	if s.localStorage != nil && videoURL != "" {
		downloadResult, err := s.localStorage.DownloadFromURLWithPath(videoURL, "videos")
		if err != nil {
			s.log.Warnw("Failed to download video to local storage",
				"error", err,
				"id", videoGenID,
				"original_url", videoURL)
		} else {
			localVideoPath = &downloadResult.RelativePath
			s.log.Infow("Video downloaded to local storage",
				"id", videoGenID,
				"original_url", videoURL,
				"local_path", downloadResult.RelativePath)
		}
	}

	// 如果视频已下载到本地，探测真实时长
	// 特别是当 AI 服务返回的 duration 为 0 或 nil 时，必须探测
	shouldProbe := localVideoPath != nil && s.ffmpeg != nil && (duration == nil || *duration == 0)
	if shouldProbe {
		absPath := s.localStorage.GetAbsolutePath(*localVideoPath)
		if probedDuration, err := s.ffmpeg.GetVideoDuration(absPath); err == nil {
			// 转换为整数秒（向上取整）
			durationInt := int(probedDuration + 0.5)
			duration = &durationInt
			s.log.Infow("Probed video duration (was 0 or nil)",
				"id", videoGenID,
				"duration_seconds", durationInt,
				"duration_float", probedDuration)
		} else {
			s.log.Errorw("Failed to probe video duration, duration will be 0",
				"error", err,
				"id", videoGenID,
				"local_path", *localVideoPath)
		}
	} else if localVideoPath != nil && s.ffmpeg != nil && duration != nil && *duration > 0 {
		// 即使有 duration，也验证一下（可选）
		absPath := s.localStorage.GetAbsolutePath(*localVideoPath)
		if probedDuration, err := s.ffmpeg.GetVideoDuration(absPath); err == nil {
			durationInt := int(probedDuration + 0.5)
			if durationInt != *duration {
				s.log.Warnw("Probed duration differs from provided duration",
					"id", videoGenID,
					"provided", *duration,
					"probed", durationInt)
				// 使用探测到的时长（更准确）
				duration = &durationInt
			}
		}
	}

	// 下载首帧图片到本地存储并保存路径
	var localFirstFramePath *string
	if firstFrameURL != nil && *firstFrameURL != "" && s.localStorage != nil {
		downloadResult, err := s.localStorage.DownloadFromURLWithPath(*firstFrameURL, "video_frames")
		if err != nil {
			s.log.Warnw("Failed to download first frame to local storage",
				"error", err,
				"id", videoGenID,
				"original_url", *firstFrameURL)
		} else {
			localFirstFramePath = &downloadResult.RelativePath
			s.log.Infow("First frame downloaded to local storage",
				"id", videoGenID,
				"original_url", *firstFrameURL,
				"local_path", downloadResult.RelativePath)
		}
	}

	// 数据库中保存原始URL和本地路径
	updates := map[string]interface{}{
		"status":     models.VideoStatusCompleted,
		"video_url":  videoURL,
		"local_path": localVideoPath,
	}
	// 只有当 duration 大于 0 时才保存，避免保存无效的 0 值
	if duration != nil && *duration > 0 {
		updates["duration"] = *duration
	}
	if width != nil {
		updates["width"] = *width
	}
	if height != nil {
		updates["height"] = *height
	}
	if firstFrameURL != nil {
		updates["first_frame_url"] = *firstFrameURL
	}
	if localFirstFramePath != nil {
		updates["first_frame_local_path"] = *localFirstFramePath
	}

	if err := s.db.Model(&models.VideoGeneration{}).Where("id = ?", videoGenID).Updates(updates).Error; err != nil {
		s.log.Errorw("Failed to update video generation", "error", err, "id", videoGenID)
		return
	}

	var videoGen models.VideoGeneration
	if err := s.db.First(&videoGen, videoGenID).Error; err == nil {
		if videoGen.StoryboardID != nil {
			// 更新 Storyboard 的 video_url、video_local_path 和 duration
			storyboardUpdates := map[string]interface{}{
				"video_url": videoURL,
			}
			// 保存视频本地路径
			if localVideoPath != nil {
				storyboardUpdates["video_local_path"] = *localVideoPath
			}
			// 只有当 duration 大于 0 时才更新，避免用无效的 0 值覆盖
			if duration != nil && *duration > 0 {
				storyboardUpdates["duration"] = *duration
			}
			if err := s.db.Model(&models.Storyboard{}).Where("id = ?", *videoGen.StoryboardID).Updates(storyboardUpdates).Error; err != nil {
				s.log.Warnw("Failed to update storyboard", "storyboard_id", *videoGen.StoryboardID, "error", err)
			} else {
				s.log.Infow("Updated storyboard with video info", "storyboard_id", *videoGen.StoryboardID, "duration", duration, "local_path", localVideoPath)
			}
		}
	}

	s.log.Infow("Video generation completed", "id", videoGenID, "url", videoURL, "duration", duration)
}

func (s *VideoGenerationService) updateVideoGenError(videoGenID uint, errorMsg string) {
	if err := s.db.Model(&models.VideoGeneration{}).Where("id = ?", videoGenID).Updates(map[string]interface{}{
		"status":    models.VideoStatusFailed,
		"error_msg": errorMsg,
	}).Error; err != nil {
		s.log.Errorw("Failed to update video generation error", "error", err, "id", videoGenID)
	}
}

func (s *VideoGenerationService) getVideoClient(provider string, modelName string) (video.VideoClient, error) {
	// 根据模型名称获取AI配置
	var config *models.AIServiceConfig
	var err error

	if modelName != "" {
		config, err = s.aiService.GetConfigForModel("video", modelName)
		if err != nil {
			s.log.Warnw("Failed to get config for model, using default", "model", modelName, "error", err)
			config, err = s.aiService.GetDefaultConfig("video")
			if err != nil {
				return nil, fmt.Errorf("no video AI config found: %w", err)
			}
		}
	} else {
		config, err = s.aiService.GetDefaultConfig("video")
		if err != nil {
			return nil, fmt.Errorf("no video AI config found: %w", err)
		}
	}

	// 使用配置中的信息创建客户端
	baseURL := config.BaseURL
	apiKey := config.APIKey
	model := modelName
	if model == "" && len(config.Model) > 0 {
		model = config.Model[0]
	}

	// 根据配置中的 provider 创建对应的客户端
	var endpoint string
	var queryEndpoint string

	switch config.Provider {
	case "chatfire":
		endpoint = "/video/generations"
		queryEndpoint = "/video/task/{taskId}"
		return video.NewChatfireClient(baseURL, apiKey, model, endpoint, queryEndpoint), nil
	case "doubao", "volcengine", "volces":
		endpoint = "/contents/generations/tasks"
		queryEndpoint = "/contents/generations/tasks/{taskId}"
		return video.NewVolcesArkClient(baseURL, apiKey, model, endpoint, queryEndpoint), nil
	case "openai":
		// OpenAI Sora 使用 /v1/videos 端点
		return video.NewOpenAISoraClient(baseURL, apiKey, model), nil
	case "runway":
		return video.NewRunwayClient(baseURL, apiKey, model), nil
	case "pika":
		return video.NewPikaClient(baseURL, apiKey, model), nil
	case "minimax":
		return video.NewMinimaxClient(baseURL, apiKey, model), nil
	default:
		return nil, fmt.Errorf("unsupported video provider: %s", provider)
	}
}

func (s *VideoGenerationService) RecoverPendingTasks() {
	var pendingVideos []models.VideoGeneration
	// Query for pending tasks with non-empty task_id
	// Note: Using IS NOT NULL and != '' to ensure we only get valid task IDs
	if err := s.db.Where("status = ? AND task_id IS NOT NULL AND task_id != ''", models.VideoStatusProcessing).Find(&pendingVideos).Error; err != nil {
		s.log.Errorw("Failed to load pending video tasks", "error", err)
		return
	}

	s.log.Infow("Recovering pending video generation tasks", "count", len(pendingVideos))

	for _, videoGen := range pendingVideos {
		// CRITICAL FIX: Check for nil TaskID before dereferencing to prevent panic
		// Even though we filter for non-empty task_id, GORM might still return nil pointers
		// This nil check prevents a potential runtime panic
		if videoGen.TaskID == nil || *videoGen.TaskID == "" {
			s.log.Warnw("Skipping video generation with nil or empty TaskID", "id", videoGen.ID)
			continue
		}

		// Start goroutine to poll task status for each pending video
		// Each goroutine will poll independently until completion or timeout
		go s.pollTaskStatus(videoGen.ID, *videoGen.TaskID, videoGen.Provider, videoGen.Model)
	}
}

func (s *VideoGenerationService) GetVideoGeneration(id uint) (*models.VideoGeneration, error) {
	var videoGen models.VideoGeneration
	if err := s.db.First(&videoGen, id).Error; err != nil {
		return nil, err
	}
	return &videoGen, nil
}

func (s *VideoGenerationService) ListVideoGenerations(dramaID *uint, storyboardID *uint, status string, limit int, offset int) ([]*models.VideoGeneration, int64, error) {
	var videos []*models.VideoGeneration
	var total int64

	query := s.db.Model(&models.VideoGeneration{})

	if dramaID != nil {
		query = query.Where("drama_id = ?", *dramaID)
	}
	if storyboardID != nil {
		query = query.Where("storyboard_id = ?", *storyboardID)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&videos).Error; err != nil {
		return nil, 0, err
	}

	return videos, total, nil
}

func (s *VideoGenerationService) GenerateVideoFromImage(imageGenID uint) (*models.VideoGeneration, error) {
	var imageGen models.ImageGeneration
	if err := s.db.First(&imageGen, imageGenID).Error; err != nil {
		return nil, fmt.Errorf("image generation not found")
	}

	if imageGen.Status != models.ImageStatusCompleted || imageGen.ImageURL == nil {
		return nil, fmt.Errorf("image is not ready")
	}

	// 获取关联的Storyboard以获取时长
	var duration *int
	if imageGen.StoryboardID != nil {
		var storyboard models.Storyboard
		if err := s.db.Where("id = ?", *imageGen.StoryboardID).First(&storyboard).Error; err == nil {
			duration = &storyboard.Duration
			s.log.Infow("Using storyboard duration for video generation",
				"storyboard_id", *imageGen.StoryboardID,
				"duration", storyboard.Duration)
		}
	}

	req := &GenerateVideoRequest{
		DramaID:      fmt.Sprintf("%d", imageGen.DramaID),
		StoryboardID: imageGen.StoryboardID,
		ImageGenID:   &imageGenID,
		ImageURL:     *imageGen.ImageURL,
		Prompt:       imageGen.Prompt,
		Provider:     "doubao",
		Duration:     duration,
	}

	return s.GenerateVideo(req)
}

func (s *VideoGenerationService) BatchGenerateVideosForEpisode(episodeID string) ([]*models.VideoGeneration, error) {
	var episode models.Episode
	if err := s.db.Preload("Storyboards").Where("id = ?", episodeID).First(&episode).Error; err != nil {
		return nil, fmt.Errorf("episode not found")
	}

	var results []*models.VideoGeneration
	for _, storyboard := range episode.Storyboards {
		if storyboard.ImagePrompt == nil {
			continue
		}

		var imageGen models.ImageGeneration
		if err := s.db.Where("storyboard_id = ? AND status = ?", storyboard.ID, models.ImageStatusCompleted).
			Order("created_at DESC").First(&imageGen).Error; err != nil {
			s.log.Warnw("No completed image for storyboard", "storyboard_id", storyboard.ID)
			continue
		}

		videoGen, err := s.GenerateVideoFromImage(imageGen.ID)
		if err != nil {
			s.log.Errorw("Failed to generate video", "storyboard_id", storyboard.ID, "error", err)
			continue
		}

		results = append(results, videoGen)
	}

	return results, nil
}

func (s *VideoGenerationService) DeleteVideoGeneration(id uint) error {
	return s.db.Delete(&models.VideoGeneration{}, id).Error
}

// convertImageToBase64 将图片转换为base64格式
// 优先使用本地存储的图片，如果没有则使用URL
func (s *VideoGenerationService) convertImageToBase64(imageURL string) (string, error) {
	// 如果已经是base64格式，直接返回
	if strings.HasPrefix(imageURL, "data:") {
		return imageURL, nil
	}

	// 尝试从本地存储读取
	if s.localStorage != nil {
		var relativePath string

		// 1. 检查是否是本地URL（包含 /static/）
		if strings.Contains(imageURL, "/static/") {
			// 提取相对路径，例如从 "http://localhost:5678/static/images/xxx.jpg" 提取 "images/xxx.jpg"
			parts := strings.Split(imageURL, "/static/")
			if len(parts) == 2 {
				relativePath = parts[1]
			}
		} else if !strings.HasPrefix(imageURL, "http://") && !strings.HasPrefix(imageURL, "https://") && !strings.HasPrefix(imageURL, "/") {
			// 2. 如果不是 HTTP/HTTPS URL 也不是绝对路径，视为相对路径（如 "images/xxx.jpg"）
			relativePath = imageURL
		} else if strings.HasPrefix(imageURL, "/") {
			// 3. 绝对路径（如 "/tmp/xxx"），直接使用
			relativePath = ""
		}

		// 如果识别出相对路径，尝试读取本地文件
		if relativePath != "" {
			absPath := s.localStorage.GetAbsolutePath(relativePath)

			// 使用工具函数转换为base64
			base64Str, err := utils.ImageToBase64(absPath)
			if err == nil {
				s.log.Infow("Converted local image to base64", "path", relativePath)
				return base64Str, nil
			}
			s.log.Warnw("Failed to convert local image to base64, will try URL", "error", err, "path", absPath)
		}
	}

	// 如果本地读取失败或不是本地路径，尝试从URL下载并转换
	base64Str, err := utils.ImageToBase64(imageURL)
	if err != nil {
		return "", fmt.Errorf("failed to convert image to base64: %w", err)
	}

	urlLen := len(imageURL)
	if urlLen > 50 {
		urlLen = 50
	}
	s.log.Infow("Converted remote image to base64", "url", imageURL[:urlLen])
	return base64Str, nil
}


// KeyframeSequenceVideoRequest 关键帧序列视频生成请求
type KeyframeSequenceVideoRequest struct {
	StoryboardID   uint        `json:"storyboard_id" binding:"required"`
	DramaID        interface{} `json:"drama_id"`
	FrameImageIDs  []uint      `json:"frame_image_ids" binding:"required,min=2"`
	VideoPrompts   []string    `json:"video_prompts" binding:"required"`
	GenerationMode string      `json:"generation_mode"` // "parallel", "sequential", "keyframe_parallel"
	Model          string      `json:"model"`
	Provider       string      `json:"provider"`
	Durations      []int       `json:"durations,omitempty"` // AI建议的视频时长（秒）
}

// GenerateKeyframeVideoPrompts 生成关键帧视频提示词
func (s *VideoGenerationService) GenerateKeyframeVideoPrompts(storyboardID uint, frameImageIDs []uint, generationMode string) ([]string, []int, error) {
	if len(frameImageIDs) < 2 {
		return nil, nil, fmt.Errorf("至少需要2帧图片")
	}

	// 获取帧图片
	var frameImages []models.ImageGeneration
	if err := s.db.Where("id IN ?", frameImageIDs).Order("id ASC").Find(&frameImages).Error; err != nil {
		return nil, nil, fmt.Errorf("查询帧图片失败: %w", err)
	}

	if len(frameImages) < 2 {
		return nil, nil, fmt.Errorf("未找到足够的帧图片")
	}

	// 获取镜头信息
	var storyboard models.Storyboard
	if err := s.db.First(&storyboard, storyboardID).Error; err != nil {
		return nil, nil, fmt.Errorf("镜头不存在")
	}

	// 构建上下文
	var contextParts []string
	if storyboard.Description != nil {
		contextParts = append(contextParts, *storyboard.Description)
	}
	if storyboard.Action != nil {
		contextParts = append(contextParts, "动作: "+*storyboard.Action)
	}
	context := strings.Join(contextParts, "; ")

	// 构建帧描述列表
	var frameDescriptions []string
	for _, img := range frameImages {
		if img.Prompt != "" {
			frameDescriptions = append(frameDescriptions, img.Prompt)
		} else {
			frameDescriptions = append(frameDescriptions, "帧画面")
		}
	}

	isKeyframeParallel := generationMode == "keyframe_parallel"

	// 调用AI生成视频提示词
	var userPrompt string
	if isKeyframeParallel {
		userPrompt = s.promptI18n.GetKeyframeParallelVideoPrompts(frameDescriptions, context)
	} else {
		userPrompt = s.promptI18n.GetKeyframeVideoPrompts(frameDescriptions, context)
	}

	aiResponse, err := s.aiService.GenerateText(userPrompt, "")
	if err != nil {
		return nil, nil, fmt.Errorf("AI生成失败: %w", err)
	}

	// 提取纯JSON内容（去除可能的markdown代码块包裹）
	jsonStr := aiResponse
	if idx := strings.Index(jsonStr, "{"); idx >= 0 {
		jsonStr = jsonStr[idx:]
	}
	if idx := strings.LastIndex(jsonStr, "}"); idx >= 0 {
		jsonStr = jsonStr[:idx+1]
	}

	// 解析JSON响应
	var result struct {
		Prompts   []string `json:"prompts"`
		Durations []int    `json:"durations"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		s.log.Warnw("Failed to parse AI response as JSON, using fallback", "error", err)
		if isKeyframeParallel {
			result.Prompts = s.generateFallbackKeyframeParallelPrompts(frameDescriptions)
		} else {
			result.Prompts = s.generateFallbackVideoPrompts(frameDescriptions)
		}
	}

	// 校验并修正提示词数量
	var expectedCount int
	if isKeyframeParallel {
		expectedCount = len(frameImages) // 每帧一个提示词
	} else {
		expectedCount = len(frameImages) - 1 // 每对帧一个提示词
	}

	if len(result.Prompts) > expectedCount {
		result.Prompts = result.Prompts[:expectedCount]
	} else if len(result.Prompts) < expectedCount {
		var fallback []string
		if isKeyframeParallel {
			fallback = s.generateFallbackKeyframeParallelPrompts(frameDescriptions)
		} else {
			fallback = s.generateFallbackVideoPrompts(frameDescriptions)
		}
		for len(result.Prompts) < expectedCount {
			idx := len(result.Prompts)
			if idx < len(fallback) {
				result.Prompts = append(result.Prompts, fallback[idx])
			} else if isKeyframeParallel {
				result.Prompts = append(result.Prompts, fmt.Sprintf("第%d帧画面的动作表现", idx+1))
			} else {
				result.Prompts = append(result.Prompts, fmt.Sprintf("从第%d帧到第%d帧的流畅过渡", idx+1, idx+2))
			}
		}
	}

	// 校验durations
	if isKeyframeParallel {
		if len(result.Durations) < expectedCount {
			for len(result.Durations) < expectedCount {
				result.Durations = append(result.Durations, 5) // 默认5秒
			}
		}
		if len(result.Durations) > expectedCount {
			result.Durations = result.Durations[:expectedCount]
		}
		// 限制时长范围 1-10 秒
		for i := range result.Durations {
			if result.Durations[i] < 1 {
				result.Durations[i] = 1
			}
			if result.Durations[i] > 10 {
				result.Durations[i] = 10
			}
		}
	}

	return result.Prompts, result.Durations, nil
}

// generateFallbackVideoPrompts 生成fallback视频提示词
func (s *VideoGenerationService) generateFallbackVideoPrompts(frameDescriptions []string) []string {
	var prompts []string
	for i := 0; i < len(frameDescriptions)-1; i++ {
		prompts = append(prompts, fmt.Sprintf("从第%d帧到第%d帧的流畅过渡，自然动作，电影级运镜", i+1, i+2))
	}
	return prompts
}

// generateFallbackKeyframeParallelPrompts 关键帧并行模式的fallback提示词
func (s *VideoGenerationService) generateFallbackKeyframeParallelPrompts(frameDescriptions []string) []string {
	var prompts []string
	for i := 0; i < len(frameDescriptions); i++ {
		prompts = append(prompts, fmt.Sprintf("第%d帧画面的动作表现，自然流畅的运动，电影级运镜", i+1))
	}
	return prompts
}

// GenerateKeyframeSequenceVideos 批量生成关键帧序列视频
func (s *VideoGenerationService) GenerateKeyframeSequenceVideos(req *KeyframeSequenceVideoRequest) (string, error) {
	if len(req.FrameImageIDs) < 2 {
		return "", fmt.Errorf("至少需要2帧图片")
	}

	// 根据生成模式校验提示词数量
	if req.GenerationMode == "keyframe_parallel" {
		if len(req.VideoPrompts) != len(req.FrameImageIDs) {
			return "", fmt.Errorf("关键帧并行模式下视频提示词数量应等于帧数")
		}
	} else {
		if len(req.VideoPrompts) != len(req.FrameImageIDs)-1 {
			return "", fmt.Errorf("视频提示词数量应为帧数-1")
		}
	}

	// 获取帧图片
	var frameImages []models.ImageGeneration
	if err := s.db.Where("id IN ?", req.FrameImageIDs).Order("id ASC").Find(&frameImages).Error; err != nil {
		return "", fmt.Errorf("查询帧图片失败: %w", err)
	}

	if len(frameImages) < 2 {
		return "", fmt.Errorf("未找到足够的帧图片")
	}

	// 按请求的ID顺序重新排序
	imageMap := make(map[uint]models.ImageGeneration)
	for _, img := range frameImages {
		imageMap[img.ID] = img
	}
	sortedImages := make([]models.ImageGeneration, len(req.FrameImageIDs))
	for i, id := range req.FrameImageIDs {
		if img, ok := imageMap[id]; ok {
			sortedImages[i] = img
		}
	}

	// 创建任务
	task, err := s.taskService.CreateTask("keyframe_videos", fmt.Sprintf("%d", req.StoryboardID))
	if err != nil {
		return "", err
	}

	// 异步处理
	if req.GenerationMode == "sequential" {
		go s.processSequentialKeyframeVideos(task.ID, req, sortedImages)
	} else if req.GenerationMode == "keyframe_parallel" {
		go s.processKeyframeParallelVideos(task.ID, req, sortedImages)
	} else {
		go s.processParallelKeyframeVideos(task.ID, req, sortedImages)
	}

	return task.ID, nil
}

// processParallelKeyframeVideos 并行生成所有视频
func (s *VideoGenerationService) processParallelKeyframeVideos(taskID string, req *KeyframeSequenceVideoRequest, frameImages []models.ImageGeneration) {
	totalVideos := len(req.VideoPrompts)
	s.taskService.UpdateTaskStatus(taskID, "processing", 0, fmt.Sprintf("开始并行生成 %d 个视频...", totalVideos))

	var wg sync.WaitGroup
	var mu sync.Mutex
	completedCount := 0
	var videoIDs []uint

	for i := 0; i < totalVideos; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			firstFrame := frameImages[index]
			lastFrame := frameImages[index+1]

			videoID, err := s.generateSingleKeyframeVideo(req, &firstFrame, &lastFrame, req.VideoPrompts[index])
			if err != nil {
				s.log.Errorw("Failed to generate keyframe video", "index", index, "error", err)
				return
			}

			mu.Lock()
			completedCount++
			videoIDs = append(videoIDs, videoID)
			progress := completedCount * 100 / totalVideos
			s.taskService.UpdateTaskStatus(taskID, "processing", progress, fmt.Sprintf("已生成 %d/%d 个视频", completedCount, totalVideos))
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	s.taskService.UpdateTaskResult(taskID, map[string]interface{}{"video_ids": videoIDs})
	if completedCount == totalVideos {
		s.taskService.UpdateTaskStatus(taskID, "completed", 100, fmt.Sprintf("成功生成 %d 个视频", totalVideos))
	} else {
		s.taskService.UpdateTaskStatus(taskID, "completed", 100, fmt.Sprintf("生成了 %d/%d 个视频", completedCount, totalVideos))
	}
}

// processKeyframeParallelVideos 关键帧并行生成：每帧图片独立生成一段视频
func (s *VideoGenerationService) processKeyframeParallelVideos(taskID string, req *KeyframeSequenceVideoRequest, frameImages []models.ImageGeneration) {
	totalVideos := len(req.VideoPrompts)
	s.taskService.UpdateTaskStatus(taskID, "processing", 0, fmt.Sprintf("开始关键帧并行生成 %d 个视频...", totalVideos))

	dramaID, _ := strconv.ParseUint(fmt.Sprintf("%v", req.DramaID), 10, 32)

	var wg sync.WaitGroup
	var mu sync.Mutex
	completedCount := 0
	var videoIDs []uint

	for i := 0; i < totalVideos; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			frame := frameImages[index]
			var frameURL string
			if frame.LocalPath != nil && *frame.LocalPath != "" {
				frameURL = *frame.LocalPath
			} else if frame.ImageURL != nil {
				frameURL = *frame.ImageURL
			}

			if frameURL == "" {
				s.log.Errorw("Frame image URL is empty", "index", index)
				return
			}

			prompt := req.VideoPrompts[index]

			videoGen := &models.VideoGeneration{
				StoryboardID:  &req.StoryboardID,
				DramaID:       uint(dramaID),
				Provider:      req.Provider,
				Prompt:        prompt,
				Model:         req.Model,
				ImageURL:      &frameURL,
				ReferenceMode: strPtr("single"),
				Status:        models.VideoStatusPending,
			}

			// 设置AI建议的时长
			if index < len(req.Durations) && req.Durations[index] > 0 {
				duration := req.Durations[index]
				videoGen.Duration = &duration
			}

			if err := s.db.Create(videoGen).Error; err != nil {
				s.log.Errorw("Failed to create video record", "index", index, "error", err)
				return
			}

			go s.ProcessVideoGeneration(videoGen.ID)

			mu.Lock()
			completedCount++
			videoIDs = append(videoIDs, videoGen.ID)
			progress := completedCount * 100 / totalVideos
			s.taskService.UpdateTaskStatus(taskID, "processing", progress, fmt.Sprintf("已提交 %d/%d 个视频生成任务", completedCount, totalVideos))
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	s.taskService.UpdateTaskResult(taskID, map[string]interface{}{"video_ids": videoIDs})
	if completedCount == totalVideos {
		s.taskService.UpdateTaskStatus(taskID, "completed", 100, fmt.Sprintf("成功生成 %d 个视频", totalVideos))
	} else {
		s.taskService.UpdateTaskStatus(taskID, "completed", 100, fmt.Sprintf("生成了 %d/%d 个视频", completedCount, totalVideos))
	}
}

// processSequentialKeyframeVideos 串行链式生成视频
// 第1段用首帧图片生成，后续每段用上一段视频的最后一帧作为首帧
func (s *VideoGenerationService) processSequentialKeyframeVideos(taskID string, req *KeyframeSequenceVideoRequest, frameImages []models.ImageGeneration) {
	totalVideos := len(req.VideoPrompts)
	s.taskService.UpdateTaskStatus(taskID, "processing", 0, fmt.Sprintf("开始串行链式生成 %d 个视频...", totalVideos))

	dramaID, _ := strconv.ParseUint(fmt.Sprintf("%v", req.DramaID), 10, 32)
	var videoIDs []uint

	// 初始首帧：用户选定的第一张图片
	var currentFirstFrameURL string
	if len(frameImages) > 0 {
		if frameImages[0].LocalPath != nil && *frameImages[0].LocalPath != "" {
			currentFirstFrameURL = *frameImages[0].LocalPath
		} else if frameImages[0].ImageURL != nil {
			currentFirstFrameURL = *frameImages[0].ImageURL
		}
	}

	for i := 0; i < totalVideos; i++ {
		s.taskService.UpdateTaskStatus(taskID, "processing", i*100/totalVideos,
			fmt.Sprintf("串行生成第 %d/%d 个视频...", i+1, totalVideos))

		if currentFirstFrameURL == "" {
			s.log.Errorw("首帧URL为空，跳过", "index", i)
			continue
		}

		// 创建视频生成记录（单图模式，只用首帧）
		prompt := req.VideoPrompts[i]
		videoGen := &models.VideoGeneration{
			StoryboardID:  &req.StoryboardID,
			DramaID:       uint(dramaID),
			Provider:      req.Provider,
			Prompt:        prompt,
			Model:         req.Model,
			ImageURL:      &currentFirstFrameURL,
			ReferenceMode: strPtr("single"),
			Status:        models.VideoStatusPending,
		}
		if err := s.db.Create(videoGen).Error; err != nil {
			s.log.Errorw("创建视频记录失败", "index", i, "error", err)
			continue
		}

		// 同步等待视频生成完成
		localVideoPath, err := s.generateAndAwaitVideo(videoGen.ID)
		if err != nil {
			s.log.Errorw("视频生成失败", "index", i, "video_gen_id", videoGen.ID, "error", err)
			continue
		}

		videoIDs = append(videoIDs, videoGen.ID)

		// 如果还有下一段，提取当前视频的最后一帧作为下一段的首帧
		if i < totalVideos-1 && localVideoPath != "" {
			absPath := localVideoPath
			if s.localStorage != nil && !strings.HasPrefix(localVideoPath, "/") {
				absPath = s.localStorage.GetAbsolutePath(localVideoPath)
			}

			frameDir := filepath.Join(os.TempDir(), "drama-keyframe-frames")
			os.MkdirAll(frameDir, 0755)
			framePath := filepath.Join(frameDir, fmt.Sprintf("task_%s_seg%d_last.png", taskID, i))

			if err := s.ffmpeg.ExtractLastFrame(absPath, framePath); err != nil {
				s.log.Errorw("提取最后一帧失败", "index", i, "error", err)
				continue
			}

			currentFirstFrameURL = framePath
			s.log.Infow("提取最后一帧成功，将作为下一段首帧", "segment", i, "frame_path", framePath)
		}
	}

	completedCount := len(videoIDs)
	s.taskService.UpdateTaskResult(taskID, map[string]interface{}{"video_ids": videoIDs})
	if completedCount == totalVideos {
		s.taskService.UpdateTaskStatus(taskID, "completed", 100, fmt.Sprintf("成功串行生成 %d 个视频", totalVideos))
	} else {
		s.taskService.UpdateTaskStatus(taskID, "completed", 100, fmt.Sprintf("串行生成了 %d/%d 个视频", completedCount, totalVideos))
	}
}

// generateAndAwaitVideo 同步等待视频生成完成，返回本地视频路径
func (s *VideoGenerationService) generateAndAwaitVideo(videoGenID uint) (string, error) {
	// 启动异步处理
	go s.ProcessVideoGeneration(videoGenID)

	// 轮询等待完成，最多等待10分钟
	maxWait := 10 * time.Minute
	interval := 5 * time.Second
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		time.Sleep(interval)

		var videoGen models.VideoGeneration
		if err := s.db.First(&videoGen, videoGenID).Error; err != nil {
			return "", fmt.Errorf("查询视频记录失败: %w", err)
		}

		switch videoGen.Status {
		case models.VideoStatusCompleted:
			localPath := ""
			if videoGen.LocalPath != nil {
				localPath = *videoGen.LocalPath
			}
			return localPath, nil
		case models.VideoStatusFailed:
			errMsg := "视频生成失败"
			if videoGen.ErrorMsg != nil {
				errMsg = *videoGen.ErrorMsg
			}
			return "", fmt.Errorf("%s", errMsg)
		}
		// still processing, continue polling
	}

	return "", fmt.Errorf("视频生成超时 (%.0f 分钟)", maxWait.Minutes())
}

// generateSingleKeyframeVideo 生成单个关键帧视频
func (s *VideoGenerationService) generateSingleKeyframeVideo(req *KeyframeSequenceVideoRequest, firstFrame, lastFrame *models.ImageGeneration, prompt string) (uint, error) {
	// 构建首尾帧URL
	var firstFrameURL, lastFrameURL string
	if firstFrame.LocalPath != nil && *firstFrame.LocalPath != "" {
		firstFrameURL = *firstFrame.LocalPath
	} else if firstFrame.ImageURL != nil {
		firstFrameURL = *firstFrame.ImageURL
	}
	if lastFrame.LocalPath != nil && *lastFrame.LocalPath != "" {
		lastFrameURL = *lastFrame.LocalPath
	} else if lastFrame.ImageURL != nil {
		lastFrameURL = *lastFrame.ImageURL
	}

	if firstFrameURL == "" || lastFrameURL == "" {
		return 0, fmt.Errorf("帧图片URL为空")
	}

	dramaID, _ := strconv.ParseUint(fmt.Sprintf("%v", req.DramaID), 10, 32)

	videoGen := &models.VideoGeneration{
		StoryboardID:   &req.StoryboardID,
		DramaID:        uint(dramaID),
		Provider:       req.Provider,
		Prompt:         prompt,
		Model:          req.Model,
		FirstFrameURL:  &firstFrameURL,
		LastFrameURL:   &lastFrameURL,
		ReferenceMode:  strPtr("first_last"),
		Status:         models.VideoStatusPending,
	}

	if err := s.db.Create(videoGen).Error; err != nil {
		return 0, fmt.Errorf("创建视频记录失败: %w", err)
	}

	// 启动异步处理
	go s.ProcessVideoGeneration(videoGen.ID)

	return videoGen.ID, nil
}
