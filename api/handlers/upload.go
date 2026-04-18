package handlers

import (
	services2 "github.com/drama-generator/backend/application/services"
	"github.com/drama-generator/backend/pkg/config"
	"github.com/drama-generator/backend/pkg/logger"
	"github.com/drama-generator/backend/pkg/response"
	"github.com/gin-gonic/gin"
)

type UploadHandler struct {
	uploadService           *services2.UploadService
	characterLibraryService *services2.CharacterLibraryService
	log                     *logger.Logger
}

func NewUploadHandler(cfg *config.Config, log *logger.Logger, characterLibraryService *services2.CharacterLibraryService) (*UploadHandler, error) {
	uploadService, err := services2.NewUploadService(cfg, log)
	if err != nil {
		return nil, err
	}

	return &UploadHandler{
		uploadService:           uploadService,
		characterLibraryService: characterLibraryService,
		log:                     log,
	}, nil
}

// UploadImage 上传图片
func (h *UploadHandler) UploadImage(c *gin.Context) {
	// 获取上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请选择文件")
		return
	}
	defer file.Close()

	// 检查文件类型
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 验证是图片类型
	allowedTypes := map[string]bool{
		"image/jpeg": true,
		"image/jpg":  true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
	}

	if !allowedTypes[contentType] {
		response.BadRequest(c, "只支持图片格式 (jpg, png, gif, webp)")
		return
	}

	// 检查文件大小 (10MB)
	if header.Size > 10*1024*1024 {
		response.BadRequest(c, "文件大小不能超过10MB")
		return
	}

	// 上传到本地存储
	result, err := h.uploadService.UploadCharacterImage(file, header.Filename, contentType)
	if err != nil {
		h.log.Errorw("Failed to upload image", "error", err)
		response.InternalError(c, "上传失败")
		return
	}

	response.Success(c, gin.H{
		"url":        result.URL,
		"local_path": result.LocalPath,
		"filename":   header.Filename,
		"size":       header.Size,
	})
}

// UploadCharacterImage 上传角色图片（带角色ID）
func (h *UploadHandler) UploadCharacterImage(c *gin.Context) {
	characterID := c.Param("id")

	// 获取上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请选择文件")
		return
	}
	defer file.Close()

	// 检查文件类型
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 验证是图片类型
	allowedTypes := map[string]bool{
		"image/jpeg": true,
		"image/jpg":  true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
	}

	if !allowedTypes[contentType] {
		response.BadRequest(c, "只支持图片格式 (jpg, png, gif, webp)")
		return
	}

	// 检查文件大小 (10MB)
	if header.Size > 10*1024*1024 {
		response.BadRequest(c, "文件大小不能超过10MB")
		return
	}

	// 上传到本地存储
	result, err := h.uploadService.UploadCharacterImage(file, header.Filename, contentType)
	if err != nil {
		h.log.Errorw("Failed to upload character image", "error", err)
		response.InternalError(c, "上传失败")
		return
	}

	// 更新角色的image_url字段到数据库
	err = h.characterLibraryService.UploadCharacterImage(characterID, result.URL)
	if err != nil {
		h.log.Errorw("Failed to update character image_url", "error", err, "character_id", characterID)
		response.InternalError(c, "更新角色图片失败")
		return
	}

	h.log.Infow("Character image uploaded and saved", "character_id", characterID, "url", result.URL, "local_path", result.LocalPath)

	response.Success(c, gin.H{
		"url":        result.URL,
		"local_path": result.LocalPath,
		"filename":   header.Filename,
		"size":       header.Size,
	})
}


// UploadVideo 上传视频文件
func (h *UploadHandler) UploadVideo(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请选择文件")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	allowedTypes := map[string]bool{
		"video/mp4":        true,
		"video/webm":       true,
		"video/quicktime":  true,
		"video/x-msvideo":  true,
		"video/x-matroska": true,
	}
	if !allowedTypes[contentType] {
		response.BadRequest(c, "只支持视频格式 (mp4, webm, mov, avi, mkv)")
		return
	}

	if header.Size > 100*1024*1024 {
		response.BadRequest(c, "视频文件大小不能超过100MB")
		return
	}

	result, err := h.uploadService.UploadFile(file, header.Filename, contentType, "videos")
	if err != nil {
		h.log.Errorw("Failed to upload video", "error", err)
		response.InternalError(c, "上传失败")
		return
	}

	response.Success(c, gin.H{
		"url":        result.URL,
		"local_path": result.LocalPath,
		"filename":   header.Filename,
		"size":       header.Size,
	})
}

// UploadAudio 上传音频文件
func (h *UploadHandler) UploadAudio(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请选择文件")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	allowedTypes := map[string]bool{
		"audio/mpeg": true,
		"audio/wav":  true,
		"audio/x-wav": true,
		"audio/aac":  true,
		"audio/mp4":  true,
		"audio/x-m4a": true,
		"audio/ogg":  true,
		"audio/webm": true,
	}
	if !allowedTypes[contentType] {
		response.BadRequest(c, "只支持音频格式 (mp3, wav, aac, m4a, ogg)")
		return
	}

	if header.Size > 30*1024*1024 {
		response.BadRequest(c, "音频文件大小不能超过30MB")
		return
	}

	result, err := h.uploadService.UploadFile(file, header.Filename, contentType, "audios")
	if err != nil {
		h.log.Errorw("Failed to upload audio", "error", err)
		response.InternalError(c, "上传失败")
		return
	}

	response.Success(c, gin.H{
		"url":        result.URL,
		"local_path": result.LocalPath,
		"filename":   header.Filename,
		"size":       header.Size,
	})
}
