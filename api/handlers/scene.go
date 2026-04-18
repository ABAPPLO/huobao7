package handlers

import (
	"strconv"

	services2 "github.com/drama-generator/backend/application/services"
	"github.com/drama-generator/backend/pkg/logger"
	"github.com/drama-generator/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type SceneHandler struct {
	sceneService  *services2.StoryboardCompositionService
	imageService  *services2.ImageGenerationService
	log           *logger.Logger
}

func NewSceneHandler(db *gorm.DB, log *logger.Logger, imageGenService *services2.ImageGenerationService) *SceneHandler {
	return &SceneHandler{
		sceneService:  services2.NewStoryboardCompositionService(db, log, imageGenService),
		imageService:  imageGenService,
		log:           log,
	}
}

func (h *SceneHandler) GetStoryboardsForEpisode(c *gin.Context) {
	episodeID := c.Param("episode_id")

	storyboards, err := h.sceneService.GetScenesForEpisode(episodeID)
	if err != nil {
		h.log.Errorw("Failed to get storyboards for episode", "error", err, "episode_id", episodeID)
		response.InternalError(c, err.Error())
		return
	}

	response.Success(c, gin.H{
		"storyboards": storyboards,
		"total":       len(storyboards),
	})
}

func (h *SceneHandler) UpdateScene(c *gin.Context) {
	sceneID := c.Param("scene_id")

	var req services2.UpdateSceneInfoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}

	if err := h.sceneService.UpdateSceneInfo(sceneID, &req); err != nil {
		h.log.Errorw("Failed to update scene", "error", err, "scene_id", sceneID)
		response.InternalError(c, err.Error())
		return
	}

	response.Success(c, gin.H{"message": "Scene updated successfully"})
}

func (h *SceneHandler) GenerateSceneImage(c *gin.Context) {
	var req services2.GenerateSceneImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}

	imageGen, err := h.sceneService.GenerateSceneImage(&req)
	if err != nil {
		h.log.Errorw("Failed to generate scene image", "error", err)
		response.InternalError(c, err.Error())
		return
	}

	response.Success(c, gin.H{
		"message":          "Scene image generation started",
		"image_generation": imageGen,
	})
}

func (h *SceneHandler) UpdateScenePrompt(c *gin.Context) {
	sceneID := c.Param("scene_id")

	var req services2.UpdateScenePromptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}

	if err := h.sceneService.UpdateScenePrompt(sceneID, &req); err != nil {
		h.log.Errorw("Failed to update scene prompt", "error", err, "scene_id", sceneID)
		if err.Error() == "scene not found" {
			response.NotFound(c, "场景不存在")
			return
		}
		response.InternalError(c, err.Error())
		return
	}

	response.Success(c, gin.H{"message": "场景提示词已更新"})
}

func (h *SceneHandler) DeleteScene(c *gin.Context) {
	sceneID := c.Param("scene_id")

	if err := h.sceneService.DeleteScene(sceneID); err != nil {
		h.log.Errorw("Failed to delete scene", "error", err, "scene_id", sceneID)
		if err.Error() == "scene not found" {
			response.NotFound(c, "场景不存在")
			return
		}
		response.InternalError(c, err.Error())
		return
	}

	response.Success(c, gin.H{"message": "场景已删除"})
}

func (h *SceneHandler) CreateScene(c *gin.Context) {
	var req services2.CreateSceneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}

	if req.DramaID == 0 {
		response.BadRequest(c, "drama_id is required")
		return
	}

	scene, err := h.sceneService.CreateScene(&req)
	if err != nil {
		h.log.Errorw("Failed to create scene", "error", err)
		response.InternalError(c, err.Error())
		return
	}

	response.Success(c, scene)
}

func (h *SceneHandler) GenerateMultiAngleSceneImage(c *gin.Context) {
	var req services2.GenerateMultiAngleSceneImagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}

	taskID, imageGens, err := h.sceneService.GenerateMultiAngleSceneImages(&req)
	if err != nil {
		h.log.Errorw("Failed to generate multi-angle scene images", "error", err)
		response.InternalError(c, err.Error())
		return
	}

	response.Success(c, gin.H{
		"message":           "多角度场景图片生成已开始",
		"task_id":           taskID,
		"image_generations": imageGens,
	})
}

func (h *SceneHandler) GetSceneAngleImages(c *gin.Context) {
	sceneIDStr := c.Param("scene_id")
	sceneID, err := strconv.ParseUint(sceneIDStr, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid scene ID")
		return
	}

	images, err := h.imageService.GetAngleImagesForScene(uint(sceneID))
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}

	response.Success(c, gin.H{
		"images": images,
		"total":  len(images),
	})
}
