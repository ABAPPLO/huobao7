package services

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/drama-generator/backend/domain/models"
	"github.com/drama-generator/backend/infrastructure/storage"
	"github.com/drama-generator/backend/pkg/config"
	"github.com/drama-generator/backend/pkg/logger"
	"gorm.io/gorm"
)

// FramePromptService 处理帧提示词生成
type FramePromptService struct {
	db              *gorm.DB
	aiService       *AIService
	log             *logger.Logger
	config          *config.Config
	promptI18n      *PromptI18n
	taskService     *TaskService
	imageGenService *ImageGenerationService
	localStorage    *storage.LocalStorage
}

// NewFramePromptService 创建帧提示词服务
func NewFramePromptService(db *gorm.DB, cfg *config.Config, log *logger.Logger) *FramePromptService {
	return &FramePromptService{
		db:          db,
		aiService:   NewAIService(db, log),
		log:         log,
		config:      cfg,
		promptI18n:  NewPromptI18n(cfg),
		taskService: NewTaskService(db, log),
	}
}

// NewFramePromptServiceWithDeps 创建带图片生成依赖的帧提示词服务
func NewFramePromptServiceWithDeps(db *gorm.DB, cfg *config.Config, log *logger.Logger, imageGenService *ImageGenerationService, localStorage *storage.LocalStorage) *FramePromptService {
	return &FramePromptService{
		db:              db,
		aiService:       NewAIService(db, log),
		log:             log,
		config:          cfg,
		promptI18n:      NewPromptI18n(cfg),
		taskService:     NewTaskService(db, log),
		imageGenService: imageGenService,
		localStorage:    localStorage,
	}
}

// FrameType 帧类型
type FrameType string

const (
	FrameTypeFirst  FrameType = "first"  // 首帧
	FrameTypeKey    FrameType = "key"    // 关键帧
	FrameTypeLast   FrameType = "last"   // 尾帧
	FrameTypePanel  FrameType = "panel"  // 分镜板（3格组合）
	FrameTypeAction FrameType = "action" // 动作序列（5格）
)

// GenerateFramePromptRequest 生成帧提示词请求
type GenerateFramePromptRequest struct {
	StoryboardID string    `json:"storyboard_id"`
	FrameType    FrameType `json:"frame_type"`
	// 可选参数
	PanelCount int `json:"panel_count,omitempty"` // 分镜板格数，默认3
	GridType   int `json:"grid_type,omitempty"`   // 动作序列宫格数，默认9
}

// FramePromptResponse 帧提示词响应
type FramePromptResponse struct {
	FrameType   FrameType          `json:"frame_type"`
	SingleFrame *SingleFramePrompt `json:"single_frame,omitempty"` // 单帧提示词
	MultiFrame  *MultiFramePrompt  `json:"multi_frame,omitempty"`  // 多帧提示词
}

// SingleFramePrompt 单帧提示词
type SingleFramePrompt struct {
	Prompt      string `json:"prompt"`
	Description string `json:"description"`
}

// MultiFramePrompt 多帧提示词
type MultiFramePrompt struct {
	Layout string              `json:"layout"` // horizontal_3, grid_2x2 等
	Frames []SingleFramePrompt `json:"frames"`
}

// GenerateFramePrompt 生成指定类型的帧提示词并保存到frame_prompts表
func (s *FramePromptService) GenerateFramePrompt(req GenerateFramePromptRequest, model string) (string, error) {
	// 查询分镜信息，预加载角色和道具
	var storyboard models.Storyboard
	if err := s.db.Preload("Characters").Preload("Props").First(&storyboard, req.StoryboardID).Error; err != nil {
		return "", fmt.Errorf("storyboard not found: %w", err)
	}

	// 创建任务
	task, err := s.taskService.CreateTask("frame_prompt_generation", req.StoryboardID)
	if err != nil {
		s.log.Errorw("Failed to create frame prompt generation task", "error", err, "storyboard_id", req.StoryboardID)
		return "", fmt.Errorf("创建任务失败: %w", err)
	}

	// 异步处理帧提示词生成
	go s.processFramePromptGeneration(task.ID, req, model)

	s.log.Infow("Frame prompt generation task created", "task_id", task.ID, "storyboard_id", req.StoryboardID, "frame_type", req.FrameType)
	return task.ID, nil
}

// processFramePromptGeneration 异步处理帧提示词生成
func (s *FramePromptService) processFramePromptGeneration(taskID string, req GenerateFramePromptRequest, model string) {
	// 更新任务状态为处理中
	s.taskService.UpdateTaskStatus(taskID, "processing", 0, "正在生成帧提示词...")

	// 查询分镜信息，预加载角色和道具
	var storyboard models.Storyboard
	if err := s.db.Preload("Characters").Preload("Props").First(&storyboard, req.StoryboardID).Error; err != nil {
		s.log.Errorw("Storyboard not found during frame prompt generation", "error", err, "storyboard_id", req.StoryboardID)
		s.taskService.UpdateTaskStatus(taskID, "failed", 0, "分镜信息不存在")
		return
	}

	// 获取场景信息
	var scene *models.Scene
	if storyboard.SceneID != nil {
		scene = &models.Scene{}
		if err := s.db.First(scene, *storyboard.SceneID).Error; err != nil {
			s.log.Warnw("Scene not found during frame prompt generation", "scene_id", *storyboard.SceneID, "task_id", taskID)
			scene = nil
		}
	}

	// 获取 drama 的 style 信息
	var episode models.Episode
	if err := s.db.Preload("Drama").First(&episode, storyboard.EpisodeID).Error; err != nil {
		s.log.Warnw("Failed to load episode and drama", "error", err, "episode_id", storyboard.EpisodeID)
	}
	dramaStyle := episode.Drama.Style

	response := &FramePromptResponse{
		FrameType: req.FrameType,
	}

	// 生成提示词
	switch req.FrameType {
	case FrameTypeFirst:
		response.SingleFrame = s.generateFirstFrame(storyboard, scene, dramaStyle, model)
		// 保存单帧提示词
		s.saveFramePrompt(req.StoryboardID, string(req.FrameType), response.SingleFrame.Prompt, response.SingleFrame.Description, "")
	case FrameTypeKey:
		response.SingleFrame = s.generateKeyFrame(storyboard, scene, dramaStyle, model)
		s.saveFramePrompt(req.StoryboardID, string(req.FrameType), response.SingleFrame.Prompt, response.SingleFrame.Description, "")
	case FrameTypeLast:
		response.SingleFrame = s.generateLastFrame(storyboard, scene, dramaStyle, model)
		s.saveFramePrompt(req.StoryboardID, string(req.FrameType), response.SingleFrame.Prompt, response.SingleFrame.Description, "")
	case FrameTypePanel:
		count := req.PanelCount
		if count == 0 {
			count = 3
		}
		response.MultiFrame = s.generatePanelFrames(storyboard, scene, count, dramaStyle, model)
		// 保存多帧提示词（合并为一条记录）
		var prompts []string
		for _, frame := range response.MultiFrame.Frames {
			prompts = append(prompts, frame.Prompt)
		}
		combinedPrompt := strings.Join(prompts, "\n---\n")
		s.saveFramePrompt(req.StoryboardID, string(req.FrameType), combinedPrompt, "分镜板组合提示词", response.MultiFrame.Layout)
		case FrameTypeAction:
			gridType := req.GridType
			if gridType != 4 && gridType != 6 && gridType != 9 {
				gridType = 9
			}
			response.MultiFrame = s.generateActionSequence(storyboard, scene, dramaStyle, model, gridType)
		var prompts []string
		for _, frame := range response.MultiFrame.Frames {
			prompts = append(prompts, frame.Prompt)
		}
		combinedPrompt := strings.Join(prompts, "\n---\n")
		s.saveFramePrompt(req.StoryboardID, string(req.FrameType), combinedPrompt, "动作序列组合提示词", response.MultiFrame.Layout)
	default:
		s.log.Errorw("Unsupported frame type during frame prompt generation", "frame_type", req.FrameType, "task_id", taskID)
		s.taskService.UpdateTaskStatus(taskID, "failed", 0, "不支持的帧类型")
		return
	}

	// 更新任务状态为完成
	s.taskService.UpdateTaskResult(taskID, map[string]interface{}{
		"response":      response,
		"storyboard_id": req.StoryboardID,
		"frame_type":    string(req.FrameType),
	})

	s.log.Infow("Frame prompt generation completed", "task_id", taskID, "storyboard_id", req.StoryboardID, "frame_type", req.FrameType)
}

// saveFramePrompt 保存帧提示词到数据库
func (s *FramePromptService) saveFramePrompt(storyboardID, frameType, prompt, description, layout string) {
	framePrompt := models.FramePrompt{
		StoryboardID: uint(mustParseUint(storyboardID)),
		FrameType:    frameType,
		Prompt:       prompt,
	}

	if description != "" {
		framePrompt.Description = &description
	}
	if layout != "" {
		framePrompt.Layout = &layout
	}

	// 先删除同类型的旧记录（保持最新）
	s.db.Where("storyboard_id = ? AND frame_type = ?", storyboardID, frameType).Delete(&models.FramePrompt{})

	// 插入新记录
	if err := s.db.Create(&framePrompt).Error; err != nil {
		s.log.Warnw("Failed to save frame prompt", "error", err, "storyboard_id", storyboardID, "frame_type", frameType)
	}
}

// mustParseUint 辅助函数
func mustParseUint(s string) uint64 {
	var result uint64
	fmt.Sscanf(s, "%d", &result)
	return result
}

// generateFirstFrame 生成首帧提示词
func (s *FramePromptService) generateFirstFrame(sb models.Storyboard, scene *models.Scene, dramaStyle string, model string) *SingleFramePrompt {
	// 构建上下文信息
	contextInfo := s.buildStoryboardContext(sb, scene)

	// 使用国际化提示词
	systemPrompt := s.promptI18n.GetFirstFramePrompt(dramaStyle)
	userPrompt := s.promptI18n.FormatUserPrompt("frame_info", contextInfo)

	// 调用AI生成（如果指定了模型则使用指定的模型）
	var aiResponse string
	var err error
	if model != "" {
		client, getErr := s.aiService.GetAIClientForModel("text", model)
		if getErr != nil {
			s.log.Warnw("Failed to get client for specified model, using default", "model", model, "error", getErr)
			aiResponse, err = s.aiService.GenerateText(userPrompt, systemPrompt)
		} else {
			aiResponse, err = client.GenerateText(userPrompt, systemPrompt)
		}
	} else {
		aiResponse, err = s.aiService.GenerateText(userPrompt, systemPrompt)
	}
	if err != nil {
		s.log.Warnw("AI generation failed, using fallback", "error", err)
		// 降级方案：使用简单拼接
		fallbackPrompt := s.buildFallbackPrompt(sb, scene, "first frame, static shot")
		return &SingleFramePrompt{
			Prompt:      fallbackPrompt,
			Description: "镜头开始的静态画面，展示初始状态",
		}
	}

	// 解析AI返回的JSON
	result := s.parseFramePromptJSON(aiResponse)
	if result == nil {
		// JSON解析失败，使用降级方案
		s.log.Warnw("Failed to parse AI JSON response, using fallback", "storyboard_id", sb.ID, "response", aiResponse)
		fallbackPrompt := s.buildFallbackPrompt(sb, scene, "first frame, static shot")
		return &SingleFramePrompt{
			Prompt:      fallbackPrompt,
			Description: "镜头开始的静态画面，展示初始状态",
		}
	}

	return result
}

// generateKeyFrame 生成关键帧提示词
func (s *FramePromptService) generateKeyFrame(sb models.Storyboard, scene *models.Scene, dramaStyle string, model string) *SingleFramePrompt {
	// 构建上下文信息
	contextInfo := s.buildStoryboardContext(sb, scene)

	// 使用国际化提示词
	systemPrompt := s.promptI18n.GetKeyFramePrompt(dramaStyle)
	userPrompt := s.promptI18n.FormatUserPrompt("key_frame_info", contextInfo)

	// 调用AI生成（如果指定了模型则使用指定的模型）
	var aiResponse string
	var err error
	if model != "" {
		client, getErr := s.aiService.GetAIClientForModel("text", model)
		if getErr != nil {
			s.log.Warnw("Failed to get client for specified model, using default", "model", model, "error", getErr)
			aiResponse, err = s.aiService.GenerateText(userPrompt, systemPrompt)
		} else {
			aiResponse, err = client.GenerateText(userPrompt, systemPrompt)
		}
	} else {
		aiResponse, err = s.aiService.GenerateText(userPrompt, systemPrompt)
	}
	if err != nil {
		s.log.Warnw("AI generation failed, using fallback", "error", err)
		fallbackPrompt := s.buildFallbackPrompt(sb, scene, "key frame, dynamic action")
		return &SingleFramePrompt{
			Prompt:      fallbackPrompt,
			Description: "动作高潮瞬间，展示关键动作",
		}
	}

	// 解析AI返回的JSON
	result := s.parseFramePromptJSON(aiResponse)
	if result == nil {
		// JSON解析失败，使用降级方案
		s.log.Warnw("Failed to parse AI JSON response, using fallback", "storyboard_id", sb.ID, "response", aiResponse)
		fallbackPrompt := s.buildFallbackPrompt(sb, scene, "key frame, dynamic action")
		return &SingleFramePrompt{
			Prompt:      fallbackPrompt,
			Description: "动作高潮瞬间，展示关键动作",
		}
	}

	return result
}

// generateLastFrame 生成尾帧提示词
func (s *FramePromptService) generateLastFrame(sb models.Storyboard, scene *models.Scene, dramaStyle string, model string) *SingleFramePrompt {
	// 构建上下文信息
	contextInfo := s.buildStoryboardContext(sb, scene)

	// 使用国际化提示词
	systemPrompt := s.promptI18n.GetLastFramePrompt(dramaStyle)
	userPrompt := s.promptI18n.FormatUserPrompt("last_frame_info", contextInfo)

	// 调用AI生成（如果指定了模型则使用指定的模型）
	var aiResponse string
	var err error
	if model != "" {
		client, getErr := s.aiService.GetAIClientForModel("text", model)
		if getErr != nil {
			s.log.Warnw("Failed to get client for specified model, using default", "model", model, "error", getErr)
			aiResponse, err = s.aiService.GenerateText(userPrompt, systemPrompt)
		} else {
			aiResponse, err = client.GenerateText(userPrompt, systemPrompt)
		}
	} else {
		aiResponse, err = s.aiService.GenerateText(userPrompt, systemPrompt)
	}
	if err != nil {
		s.log.Warnw("AI generation failed, using fallback", "error", err)
		fallbackPrompt := s.buildFallbackPrompt(sb, scene, "last frame, final state")
		return &SingleFramePrompt{
			Prompt:      fallbackPrompt,
			Description: "镜头结束画面，展示最终状态和结果",
		}
	}

	// 解析AI返回的JSON
	result := s.parseFramePromptJSON(aiResponse)
	if result == nil {
		// JSON解析失败，使用降级方案
		s.log.Warnw("Failed to parse AI JSON response, using fallback", "storyboard_id", sb.ID, "response", aiResponse)
		fallbackPrompt := s.buildFallbackPrompt(sb, scene, "last frame, final state")
		return &SingleFramePrompt{
			Prompt:      fallbackPrompt,
			Description: "镜头结束画面，展示最终状态和结果",
		}
	}

	return result
}

// generatePanelFrames 生成分镜板提示词（多格组合）
func (s *FramePromptService) generatePanelFrames(sb models.Storyboard, scene *models.Scene, count int, dramaStyle string, model string) *MultiFramePrompt {
	layout := fmt.Sprintf("horizontal_%d", count)

	frames := make([]SingleFramePrompt, count)

	// 固定生成：首帧 -> 关键帧 -> 尾帧
	if count == 3 {
		frames[0] = *s.generateFirstFrame(sb, scene, dramaStyle, model)
		frames[0].Description = "第1格：初始状态"

		frames[1] = *s.generateKeyFrame(sb, scene, dramaStyle, model)
		frames[1].Description = "第2格：动作高潮"

		frames[2] = *s.generateLastFrame(sb, scene, dramaStyle, model)
		frames[2].Description = "第3格：最终状态"
	} else if count == 4 {
		// 4格：首帧 -> 中间帧1 -> 中间帧2 -> 尾帧
		frames[0] = *s.generateFirstFrame(sb, scene, dramaStyle, model)
		frames[1] = *s.generateKeyFrame(sb, scene, dramaStyle, model)
		frames[2] = *s.generateKeyFrame(sb, scene, dramaStyle, model)
		frames[3] = *s.generateLastFrame(sb, scene, dramaStyle, model)
	}

	return &MultiFramePrompt{
		Layout: layout,
		Frames: frames,
	}
}

// generateActionSequence 生成动作序列提示词（3x3宫格）
func (s *FramePromptService) generateActionSequence(sb models.Storyboard, scene *models.Scene, dramaStyle string, model string, gridType int) *MultiFramePrompt {
	// 验证宫格类型
	if gridType != 4 && gridType != 6 && gridType != 9 {
		gridType = 9
	}
	// 构建上下文信息
	contextInfo := s.buildStoryboardContext(sb, scene)

	// 使用国际化提示词 - 专门为动作序列设计的提示词
	systemPrompt := s.promptI18n.GetActionSequenceFramePrompt(dramaStyle, gridType)
	userPrompt := s.promptI18n.FormatUserPrompt("frame_info", contextInfo)

	// 调用AI生成（如果指定了模型则使用指定的模型）
	var aiResponse string
	var err error
	if model != "" {
		client, getErr := s.aiService.GetAIClientForModel("text", model)
		if getErr != nil {
			s.log.Warnw("Failed to get client for specified model, using default", "model", model, "error", getErr)
			aiResponse, err = s.aiService.GenerateText(userPrompt, systemPrompt)
		} else {
			aiResponse, err = client.GenerateText(userPrompt, systemPrompt)
		}
	} else {
		aiResponse, err = s.aiService.GenerateText(userPrompt, systemPrompt)
	}

	if err != nil {
		s.log.Warnw("AI generation failed for action sequence, using smart fallback", "error", err)
		return s.generateActionSequenceFallback(sb, scene, dramaStyle, gridType)
	}

	// 解析AI返回的JSON（根据宫格类型检查帧数）
	actionFrames := s.parseActionSequenceJSON(aiResponse)
	if actionFrames == nil || len(actionFrames) < gridType {
		// JSON解析失败或帧数不足，使用智能降级方案
		s.log.Warnw("Failed to parse AI JSON response for action sequence, using smart fallback",
			"storyboard_id", sb.ID, "parsed_frames", len(actionFrames), "expected", gridType)
		return s.generateActionSequenceFallback(sb, scene, dramaStyle, gridType)
	}

	// 根据宫格类型确定布局
	layout := "grid_3x3"
	if gridType == 4 {
		layout = "grid_2x2"
	} else if gridType == 6 {
		layout = "grid_2x3"
	}

	return &MultiFramePrompt{
		Layout: layout,
		Frames: actionFrames,
	}
}

// generateActionSequenceFallback 智能生成动作序列降级方案
func (s *FramePromptService) generateActionSequenceFallback(sb models.Storyboard, scene *models.Scene, dramaStyle string, gridType int) *MultiFramePrompt {
	baseContext := s.buildStoryboardContext(sb, scene)

	// 验证宫格类型
	if gridType != 4 && gridType != 6 && gridType != 9 {
		gridType = 9
	}

	// 9个动作阶段的描述（中文）
	allPhases := []struct {
		desc       string
		actionHint string
	}{
		{"准备", "准备姿态，身体静止，动作前的平静，就位姿势"},
		{"蓄力", "蓄力待发，身体紧绷，积蓄能量，微微下蹲或后拉"},
		{"启动", "开始启动，开始移动，最初的动势，动作开始"},
		{"加速", "加速进行，建立动量，速度提升，动态姿势"},
		{"峰值", "张力峰值，最大能量集中，即将爆发，如弹簧压紧"},
		{"爆发", "爆发瞬间，全力释放，动作高潮，最动态的瞬间"},
		{"释放", "能量释放，惯性延续，能量扩散，顺势而为"},
		{"减速", "减速过程，逐渐放慢，能量消退，恢复平衡"},
		{"收束", "收束完成，回归静止，动作结束，最终姿态"},
	}

	// 根据宫格类型选择阶段
	var phases []struct {
		desc       string
		actionHint string
	}
	if gridType == 4 {
		phases = []struct {
			desc       string
			actionHint string
		}{
			allPhases[0], // 准备
			allPhases[2], // 启动
			allPhases[5], // 爆发
			allPhases[8], // 收束
		}
	} else if gridType == 6 {
		phases = []struct {
			desc       string
			actionHint string
		}{
			allPhases[0], // 准备
			allPhases[1], // 蓄力
			allPhases[2], // 启动
			allPhases[4], // 峰值
			allPhases[5], // 爆发
			allPhases[8], // 收束
		}
	} else {
		phases = allPhases // 9帧使用全部
	}

	frames := make([]SingleFramePrompt, gridType)
	for i, phase := range phases {
		// 构建每帧的完整提示词
		// 第1帧是完整的图片生成提示词
		// 第2帧起是编辑指令
		var framePrompt string
		if i == 0 {
			framePrompt = fmt.Sprintf("%s，%s，第%d帧/共%d帧：%s阶段，%s，连续动作序列，角色一致性，1:1正方形格式，高细节",
				baseContext, dramaStyle, i+1, gridType, phase.desc, phase.actionHint)
		} else {
			framePrompt = fmt.Sprintf("编辑：%s阶段，%s，相对于前一帧的增量变化，连续动作序列",
				phase.desc, phase.actionHint)
		}

		frames[i] = SingleFramePrompt{
			Prompt:      framePrompt,
			Description: fmt.Sprintf("第%d帧：%s", i+1, phase.desc),
		}
	}

	// 根据宫格类型确定布局
	layout := "grid_3x3"
	if gridType == 4 {
		layout = "grid_2x2"
	} else if gridType == 6 {
		layout = "grid_2x3"
	}

	s.log.Infow("Generated action sequence fallback", "storyboard_id", sb.ID, "grid_type", gridType)
	return &MultiFramePrompt{
		Layout: layout,
		Frames: frames,
	}
}

// buildStoryboardContext 构建镜头上下文信息
func (s *FramePromptService) buildStoryboardContext(sb models.Storyboard, scene *models.Scene) string {
	var parts []string

	// 镜头描述（最重要）
	if sb.Description != nil && *sb.Description != "" {
		parts = append(parts, s.promptI18n.FormatUserPrompt("shot_description_label", *sb.Description))
	}

	// 场景信息
	if scene != nil {
		parts = append(parts, s.promptI18n.FormatUserPrompt("scene_label", scene.Location, scene.Time))
	} else if sb.Location != nil && sb.Time != nil {
		parts = append(parts, s.promptI18n.FormatUserPrompt("scene_label", *sb.Location, *sb.Time))
	}

	// 角色
	if len(sb.Characters) > 0 {
		var charNames []string
		for _, char := range sb.Characters {
			charNames = append(charNames, char.Name)
		}
		parts = append(parts, s.promptI18n.FormatUserPrompt("characters_label", strings.Join(charNames, ", ")))
	}

	// 道具
	if len(sb.Props) > 0 {
		var propNames []string
		for _, prop := range sb.Props {
			propNames = append(propNames, prop.Name)
		}
		parts = append(parts, s.promptI18n.FormatUserPrompt("props_label", strings.Join(propNames, ", ")))
	}

	// 动作
	if sb.Action != nil && *sb.Action != "" {
		parts = append(parts, s.promptI18n.FormatUserPrompt("action_label", *sb.Action))
	}

	// 结果
	if sb.Result != nil && *sb.Result != "" {
		parts = append(parts, s.promptI18n.FormatUserPrompt("result_label", *sb.Result))
	}

	// 对白
	if sb.Dialogue != nil && *sb.Dialogue != "" {
		parts = append(parts, s.promptI18n.FormatUserPrompt("dialogue_label", *sb.Dialogue))
	}

	// 氛围
	if sb.Atmosphere != nil && *sb.Atmosphere != "" {
		parts = append(parts, s.promptI18n.FormatUserPrompt("atmosphere_label", *sb.Atmosphere))
	}

	// 镜头参数
	if sb.ShotType != nil {
		parts = append(parts, s.promptI18n.FormatUserPrompt("shot_type_label", *sb.ShotType))
	}
	if sb.Angle != nil {
		parts = append(parts, s.promptI18n.FormatUserPrompt("angle_label", *sb.Angle))
	}
	if sb.Movement != nil {
		parts = append(parts, s.promptI18n.FormatUserPrompt("movement_label", *sb.Movement))
	}

	return strings.Join(parts, "\n")
}

// buildFallbackPrompt 构建降级提示词（AI失败时使用）
func (s *FramePromptService) buildFallbackPrompt(sb models.Storyboard, scene *models.Scene, suffix string) string {
	var parts []string

	// 场景
	if scene != nil {
		parts = append(parts, fmt.Sprintf("%s, %s", scene.Location, scene.Time))
	}

	// 角色
	if len(sb.Characters) > 0 {
		for _, char := range sb.Characters {
			parts = append(parts, char.Name)
		}
	}

	// 氛围
	if sb.Atmosphere != nil {
		parts = append(parts, *sb.Atmosphere)
	}

	parts = append(parts, "anime style", suffix)
	return strings.Join(parts, ", ")
}

// GenerateActionSequenceImages 逐帧串行生成动作序列宫格图片
func (s *FramePromptService) GenerateActionSequenceImages(storyboardID uint, dramaID string, gridType int) (string, error) {
	if s.imageGenService == nil {
		return "", fmt.Errorf("图片生成服务不可用")
	}

	// 验证宫格类型
	if gridType != 4 && gridType != 6 && gridType != 9 {
		gridType = 9 // 默认9宫格
	}

	task, err := s.taskService.CreateTask("action_sequence_images", fmt.Sprintf("%d", storyboardID))
	if err != nil {
		return "", err
	}

	go s.processActionSequenceImages(task.ID, storyboardID, dramaID, gridType)
	return task.ID, nil
}

func (s *FramePromptService) processActionSequenceImages(taskID string, storyboardID uint, dramaID string, gridType int) {
	s.taskService.UpdateTaskStatus(taskID, "processing", 0, "正在生成动作序列帧提示词...")

	var storyboard models.Storyboard
	if err := s.db.Preload("Characters").Preload("Props").First(&storyboard, storyboardID).Error; err != nil {
		s.taskService.UpdateTaskError(taskID, fmt.Errorf("镜头不存在: %w", err))
		return
	}

	var scene *models.Scene
	if storyboard.SceneID != nil {
		var s2 models.Scene
		if err := s.db.First(&s2, *storyboard.SceneID).Error; err == nil {
			scene = &s2
		}
	}

	var drama models.Drama
	if err := s.db.First(&drama, dramaID).Error; err != nil {
		s.taskService.UpdateTaskError(taskID, fmt.Errorf("项目不存在: %w", err))
		return
	}

	// 根据宫格类型计算行列
	var cols, rows int
	switch gridType {
	case 4:
		cols, rows = 2, 2
	case 6:
		cols, rows = 3, 2
	default:
		cols, rows = 3, 3
		gridType = 9
	}
	targetFrameCount := cols * rows

	// 生成帧提示词
	multiFrame := s.generateActionSequence(storyboard, scene, drama.Style, "", gridType)
	if multiFrame == nil || len(multiFrame.Frames) == 0 {
		s.taskService.UpdateTaskError(taskID, fmt.Errorf("生成帧提示词失败"))
		return
	}

	frames := multiFrame.Frames
	// 根据宫格类型截取帧数
	if len(frames) > targetFrameCount {
		frames = frames[:targetFrameCount]
	}
	totalFrames := len(frames)

	// 逐帧串行生成图片
	var generatedImagePaths []string
	var prevImageRef *string

	for i := 0; i < totalFrames; i++ {
		s.taskService.UpdateTaskStatus(taskID, "processing", (i+1)*100/(totalFrames+2),
			fmt.Sprintf("正在生成第 %d/%d 帧...", i+1, totalFrames))

		frame := frames[i]

		req := &GenerateImageRequest{
			StoryboardID:    &storyboardID,
			DramaID:         dramaID,
			ImageType:       "storyboard",
			FrameType:       strPtr("action"),
			Prompt:          frame.Prompt,
			Provider:        s.config.AI.DefaultImageProvider,
			Size:            "2K",
			ReferenceImages: []string{},
		}

		if prevImageRef != nil {
			req.ReferenceImages = []string{*prevImageRef}
		}

		imageGen, err := s.imageGenService.GenerateImage(req)
		if err != nil {
			s.log.Errorw("Failed to generate frame", "frame", i+1, "error", err)
			s.taskService.UpdateTaskError(taskID, fmt.Errorf("第 %d 帧生成失败: %w", i+1, err))
			return
		}

		completedURL, completedPath, err := s.waitForImageCompletion(imageGen.ID)
		if err != nil {
			s.taskService.UpdateTaskError(taskID, fmt.Errorf("第 %d 帧等待失败: %w", i+1, err))
			return
		}

		if completedPath != nil {
			generatedImagePaths = append(generatedImagePaths, *completedPath)
			prevImageRef = completedPath
		} else if completedURL != nil {
			generatedImagePaths = append(generatedImagePaths, *completedURL)
			prevImageRef = completedURL
		}

		s.log.Infow("Action sequence frame generated",
			"task_id", taskID, "frame", i+1, "total", totalFrames)
	}

	// 拼合九宫格
	s.taskService.UpdateTaskStatus(taskID, "processing", 95, "正在拼合宫格图片...")

	compositePath, err := s.composeGridImage(generatedImagePaths, cols, rows)
	if err != nil {
		s.taskService.UpdateTaskError(taskID, fmt.Errorf("拼合图片失败: %w", err))
		return
	}

		frameType := "action"
	imageURL := ""
	if s.localStorage != nil {
		imageURL = s.localStorage.GetURL(*compositePath)
	}

	newImageGen := &models.ImageGeneration{
		DramaID:      drama.ID,
		StoryboardID: &storyboardID,
		ImageType:    string(models.ImageTypeStoryboard),
		FrameType:    &frameType,
		Prompt:       fmt.Sprintf("动作序列九宫格 - %d帧", totalFrames),
		Status:       models.ImageStatusCompleted,
		ImageURL:     &imageURL,
		LocalPath:    compositePath,
		Provider:     s.config.AI.DefaultImageProvider,
		CompletedAt:  timePtr(time.Now()),
	}
	if err := s.db.Create(newImageGen).Error; err != nil {
		s.taskService.UpdateTaskError(taskID, fmt.Errorf("保存图片记录失败: %w", err))
		return
	}

	s.taskService.UpdateTaskResult(taskID, map[string]interface{}{
		"image_url":    imageURL,
		"local_path":   compositePath,
		"image_gen_id": newImageGen.ID,
		"total_frames": totalFrames,
	})
}

func (s *FramePromptService) waitForImageCompletion(imageGenID uint) (*string, *string, error) {
	maxAttempts := 120
	pollInterval := 3 * time.Second

	for i := 0; i < maxAttempts; i++ {
		time.Sleep(pollInterval)
		var imageGen models.ImageGeneration
		if err := s.db.First(&imageGen, imageGenID).Error; err != nil {
			continue
		}
		if imageGen.Status == models.ImageStatusCompleted {
			return imageGen.ImageURL, imageGen.LocalPath, nil
		}
		if imageGen.Status == models.ImageStatusFailed {
			errMsg := "图片生成失败"
			if imageGen.ErrorMsg != nil {
				errMsg = *imageGen.ErrorMsg
			}
			return nil, nil, fmt.Errorf(errMsg)
		}
	}
	return nil, nil, fmt.Errorf("图片生成超时")
}

func (s *FramePromptService) composeGridImage(imagePaths []string, cols, rows int) (*string, error) {
	if len(imagePaths) == 0 {
		return nil, fmt.Errorf("没有图片可拼合")
	}

	// 根据比例计算单元格尺寸，保持与首帧等帧类型一致
	cellW := 1024
	cellH := 576 // 默认 16:9
	if s.config != nil {
		cellW, cellH = parseAspectRatio("16:9", 1024)
	}
	gap := 6
	totalW := cellW*cols + gap*(cols-1)
	totalH := cellH*rows + gap*(rows-1)

	canvas := image.NewRGBA(image.Rect(0, 0, totalW, totalH))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{image.White}, image.Point{}, draw.Src)

	for i, imgPath := range imagePaths {
		if i >= cols*rows {
			break
		}
		img, err := s.loadImageFromPath(imgPath)
		if err != nil {
			s.log.Warnw("Failed to load image for grid", "path", imgPath, "error", err)
			continue
		}
		resized := s.resizeImage(img, cellW, cellH)
		col := i % cols
		row := i / cols
		x := col * (cellW + gap)
		y := row * (cellH + gap)
		draw.Draw(canvas, image.Rect(x, y, x+cellW, y+cellH), resized, image.Point{}, draw.Src)
	}

	if s.localStorage == nil {
		return nil, fmt.Errorf("本地存储不可用")
	}

	storagePath := s.localStorage.GetAbsolutePath("")
	dir := storagePath + "/images/grids"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_action_grid_%dx%d.png", timestamp, cols, rows)
	filePath := filepath.Join(dir, filename)

	f, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("创建文件失败: %w", err)
	}
	defer f.Close()

	if err := png.Encode(f, canvas); err != nil {
		return nil, fmt.Errorf("编码PNG失败: %w", err)
	}

	relPath := "images/grids/" + filename
	return &relPath, nil
}

func (s *FramePromptService) loadImageFromPath(path string) (image.Image, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(path)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return s.decodeImage(resp.Body)
	}
	absPath := path
	if s.localStorage != nil && !strings.HasPrefix(path, "/") {
		absPath = s.localStorage.GetAbsolutePath(path)
	}
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return s.decodeImage(f)
}

func (s *FramePromptService) decodeImage(r io.Reader) (image.Image, error) {
	img, _, err := image.Decode(r)
	return img, err
}

func (s *FramePromptService) resizeImage(src image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	srcBounds := src.Bounds()
	sx := float64(srcBounds.Dx()) / float64(w)
	sy := float64(srcBounds.Dy()) / float64(h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			srcX := int(math.Floor(float64(x) * sx))
			srcY := int(math.Floor(float64(y) * sy))
			if srcX >= srcBounds.Dx() {
				srcX = srcBounds.Dx() - 1
			}
			if srcY >= srcBounds.Dy() {
				srcY = srcBounds.Dy() - 1
			}
			dst.Set(x, y, src.At(srcBounds.Min.X+srcX, srcBounds.Min.Y+srcY))
		}
	}
	return dst
}

// BatchGenerateFirstFrameImages 一键批量生成剧集所有镜头的首帧图片
// 先提取提示词，再生成图片，串行处理
func (s *FramePromptService) BatchGenerateFirstFrameImages(episodeID string, model string) (string, error) {
	if s.imageGenService == nil {
		return "", fmt.Errorf("图片生成服务不可用")
	}

	// 验证剧集存在
	var episode models.Episode
	if err := s.db.First(&episode, episodeID).Error; err != nil {
		return "", fmt.Errorf("剧集不存在: %w", err)
	}

	// 创建异步任务
	task, err := s.taskService.CreateTask("batch_first_frame_images", episodeID)
	if err != nil {
		return "", fmt.Errorf("创建任务失败: %w", err)
	}

	go s.processBatchFirstFrameImages(task.ID, episodeID, model)

	s.log.Infow("Batch first-frame images task created", "task_id", task.ID, "episode_id", episodeID)
	return task.ID, nil
}

// processBatchFirstFrameImages 异步处理批量首帧图片生成
func (s *FramePromptService) processBatchFirstFrameImages(taskID string, episodeID string, model string) {
	s.taskService.UpdateTaskStatus(taskID, "processing", 0, "正在加载镜头信息...")

	// 加载剧集及其所有镜头
	var episode models.Episode
	if err := s.db.
		Preload("Drama").
		Preload("Storyboards", func(db *gorm.DB) *gorm.DB {
			return db.Order("storyboard_number ASC")
		}).
		Preload("Storyboards.Characters").
		Preload("Storyboards.Props").
		Preload("Storyboards.Background").
		First(&episode, episodeID).Error; err != nil {
		s.taskService.UpdateTaskError(taskID, fmt.Errorf("加载剧集失败: %w", err))
		return
	}

	storyboards := episode.Storyboards
	if len(storyboards) == 0 {
		s.taskService.UpdateTaskError(taskID, fmt.Errorf("该剧集没有镜头"))
		return
	}

	total := len(storyboards)
	processed := 0
	skipped := 0
	failed := 0
	var results []map[string]interface{}

	for i, storyboard := range storyboards {
		progress := (i + 1) * 100 / (total + 1)
		s.taskService.UpdateTaskStatus(taskID, "processing", progress,
			fmt.Sprintf("正在处理第 %d/%d 个镜头...", i+1, total))

		// 检查是否已有完成的首帧图片
		var existingCount int64
		s.db.Model(&models.ImageGeneration{}).
			Where("storyboard_id = ? AND frame_type = ? AND status = ?",
				storyboard.ID, "first", models.ImageStatusCompleted).
			Count(&existingCount)
		if existingCount > 0 {
			skipped++
			s.log.Infow("Skipping storyboard with existing first-frame image",
				"storyboard_id", storyboard.ID, "storyboard_number", storyboard.StoryboardNumber)
			results = append(results, map[string]interface{}{
				"storyboard_id":     storyboard.ID,
				"storyboard_number": storyboard.StoryboardNumber,
				"status":            "skipped",
			})
			continue
		}

		// 加载场景信息
		var scene *models.Scene
		if storyboard.SceneID != nil {
			var s2 models.Scene
			if err := s.db.First(&s2, *storyboard.SceneID).Error; err == nil {
				scene = &s2
			}
		}

		// 第1步：生成首帧提示词
		s.taskService.UpdateTaskStatus(taskID, "processing", progress,
			fmt.Sprintf("正在提取第 %d/%d 个镜头的首帧提示词...", i+1, total))

		framePrompt := s.generateFirstFrame(storyboard, scene, episode.Drama.Style, model)
		if framePrompt == nil || framePrompt.Prompt == "" {
			failed++
			s.log.Warnw("Failed to generate first-frame prompt",
				"storyboard_id", storyboard.ID, "storyboard_number", storyboard.StoryboardNumber)
			results = append(results, map[string]interface{}{
				"storyboard_id":     storyboard.ID,
				"storyboard_number": storyboard.StoryboardNumber,
				"status":            "failed",
				"error":             "提示词生成失败",
			})
			continue
		}

		// 保存提示词
		s.saveFramePrompt(fmt.Sprintf("%d", storyboard.ID), "first",
			framePrompt.Prompt, framePrompt.Description, "")

		// 第2步：收集参考图片（场景背景 + 角色图片）
		var referenceImages []string
		if scene != nil && scene.LocalPath != nil && *scene.LocalPath != "" {
			referenceImages = append(referenceImages, *scene.LocalPath)
		}
		for _, char := range storyboard.Characters {
			if char.LocalPath != nil && *char.LocalPath != "" {
				referenceImages = append(referenceImages, *char.LocalPath)
			}
		}

		// 第3步：生成图片
		s.taskService.UpdateTaskStatus(taskID, "processing", progress,
			fmt.Sprintf("正在生成第 %d/%d 个镜头的首帧图片...", i+1, total))

		storyboardIDUint := storyboard.ID
		frameTypeStr := "first"
		req := &GenerateImageRequest{
			StoryboardID:    &storyboardIDUint,
			DramaID:         fmt.Sprintf("%d", episode.DramaID),
			ImageType:       "storyboard",
			FrameType:       &frameTypeStr,
			Prompt:          framePrompt.Prompt,
			Provider:        s.config.AI.DefaultImageProvider,
			ReferenceImages: referenceImages,
		}

		imageGen, err := s.imageGenService.GenerateImage(req)
		if err != nil {
			failed++
			s.log.Errorw("Failed to generate first-frame image",
				"storyboard_id", storyboard.ID, "error", err)
			results = append(results, map[string]interface{}{
				"storyboard_id":     storyboard.ID,
				"storyboard_number": storyboard.StoryboardNumber,
				"status":            "failed",
				"error":             err.Error(),
			})
			continue
		}

		// 等待图片生成完成
		completedURL, completedPath, waitErr := s.waitForImageCompletion(imageGen.ID)
		if waitErr != nil {
			failed++
			s.log.Errorw("First-frame image generation failed or timed out",
				"storyboard_id", storyboard.ID, "error", waitErr)
			results = append(results, map[string]interface{}{
				"storyboard_id":     storyboard.ID,
				"storyboard_number": storyboard.StoryboardNumber,
				"status":            "failed",
				"error":             waitErr.Error(),
			})
			continue
		}

		processed++
		resultEntry := map[string]interface{}{
			"storyboard_id":     storyboard.ID,
			"storyboard_number": storyboard.StoryboardNumber,
			"prompt":            framePrompt.Prompt,
			"status":            "completed",
		}
		if completedURL != nil {
			resultEntry["image_url"] = *completedURL
		}
		if completedPath != nil {
			resultEntry["local_path"] = *completedPath
		}
		results = append(results, resultEntry)

		s.log.Infow("First-frame image generated",
			"storyboard_id", storyboard.ID,
			"storyboard_number", storyboard.StoryboardNumber,
			"progress", fmt.Sprintf("%d/%d", i+1, total))
	}

	// 任务完成
	s.taskService.UpdateTaskResult(taskID, map[string]interface{}{
		"episode_id":        episodeID,
		"total_storyboards": total,
		"processed":         processed,
		"skipped":           skipped,
		"failed":            failed,
		"results":           results,
	})

	s.log.Infow("Batch first-frame images completed",
		"task_id", taskID,
		"episode_id", episodeID,
		"total", total,
		"processed", processed,
		"skipped", skipped,
		"failed", failed)
}

// parseAspectRatio 解析长宽比字符串（如 "16:9"），返回基于基准宽度的宽高像素值
func parseAspectRatio(ratio string, baseWidth int) (int, int) {
	parts := strings.Split(ratio, ":")
	if len(parts) == 2 {
		w, err1 := strconv.Atoi(parts[0])
		h, err2 := strconv.Atoi(parts[1])
		if err1 == nil && err2 == nil && w > 0 && h > 0 {
			return baseWidth, baseWidth * h / w
		}
	}
	return baseWidth, baseWidth * 9 / 16 // 默认 16:9
}

func strPtr(s string) *string       { return &s }
func timePtr(t time.Time) *time.Time { return &t }
