package handlers

import (
	"fmt"

	"github.com/drama-generator/backend/application/services"
	"github.com/drama-generator/backend/pkg/logger"
	"github.com/drama-generator/backend/pkg/response"
	"github.com/gin-gonic/gin"
)

// FramePromptHandler 处理帧提示词生成请求
type FramePromptHandler struct {
	framePromptService *services.FramePromptService
	log                *logger.Logger
}

// NewFramePromptHandler 创建帧提示词处理器
func NewFramePromptHandler(framePromptService *services.FramePromptService, log *logger.Logger) *FramePromptHandler {
	return &FramePromptHandler{
		framePromptService: framePromptService,
		log:                log,
	}
}

// GenerateFramePrompt 生成指定类型的帧提示词
// POST /api/v1/storyboards/:id/frame-prompt
func (h *FramePromptHandler) GenerateFramePrompt(c *gin.Context) {
	storyboardID := c.Param("id")

	var req struct {
		FrameType  string `json:"frame_type"`
		PanelCount int    `json:"panel_count"`
		GridType   int    `json:"grid_type"` // 动作序列宫格数：4/6/9
		Model      string `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	serviceReq := services.GenerateFramePromptRequest{
		StoryboardID: storyboardID,
		FrameType:    services.FrameType(req.FrameType),
		PanelCount:   req.PanelCount,
		GridType:     req.GridType,
	}

	// 直接调用服务层的异步方法，该方法会创建任务并返回任务ID
	taskID, err := h.framePromptService.GenerateFramePrompt(serviceReq, req.Model)
	if err != nil {
		h.log.Errorw("Failed to generate frame prompt", "error", err)
		response.InternalError(c, err.Error())
		return
	}

	// 立即返回任务ID
	response.Success(c, gin.H{
		"task_id": taskID,
		"status":  "pending",
		"message": "帧提示词生成任务已创建，正在后台处理...",
	})
}

// GenerateActionSequenceImages 逐帧串行生成动作序列宫格图片
// POST /api/v1/storyboards/:id/action-sequence-images
func (h *FramePromptHandler) GenerateActionSequenceImages(c *gin.Context) {
	storyboardID := c.Param("id")

	var storyboardIDUint uint
	if _, err := fmt.Sscanf(storyboardID, "%d", &storyboardIDUint); err != nil {
		response.BadRequest(c, "无效的镜头ID")
		return
	}

	var req struct {
		DramaID  string `json:"drama_id"`
		GridType int    `json:"grid_type"` // 4/6/9
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if req.DramaID == "" {
		response.BadRequest(c, "drama_id 不能为空")
		return
	}

	gridType := req.GridType
	if gridType != 4 && gridType != 6 && gridType != 9 {
		gridType = 9 // 默认9宫格
	}

	taskID, err := h.framePromptService.GenerateActionSequenceImages(storyboardIDUint, req.DramaID, gridType)
	if err != nil {
		h.log.Errorw("Failed to generate action sequence images", "error", err)
		response.InternalError(c, err.Error())
		return
	}

	response.Success(c, gin.H{
		"task_id": taskID,
		"status":  "pending",
		"message": "动作序列图片生成任务已创建，正在后台处理...",
	})
}
