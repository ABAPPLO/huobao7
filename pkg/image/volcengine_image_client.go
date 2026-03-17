package image

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type VolcEngineImageClient struct {
	BaseURL       string
	APIKey        string
	Model         string
	Endpoint      string
	QueryEndpoint string
	HTTPClient    *http.Client
}

type VolcEngineImageRequest struct {
	Model                     string   `json:"model"`
	Prompt                    string   `json:"prompt"`
	Image                     []string `json:"image,omitempty"`
	SequentialImageGeneration string   `json:"sequential_image_generation,omitempty"`
	Size                      string   `json:"size,omitempty"`
	Watermark                 bool     `json:"watermark,omitempty"`
}

type VolcEngineImageResponse struct {
	Model   string `json:"model"`
	Created int64  `json:"created"`
	Data    []struct {
		URL  string `json:"url"`
		Size string `json:"size"`
	} `json:"data"`
	Usage struct {
		GeneratedImages int `json:"generated_images"`
		OutputTokens    int `json:"output_tokens"`
		TotalTokens     int `json:"total_tokens"`
	} `json:"usage"`
	Error interface{} `json:"error,omitempty"`
}

func NewVolcEngineImageClient(baseURL, apiKey, model, endpoint, queryEndpoint string) *VolcEngineImageClient {
	if endpoint == "" {
		endpoint = "/api/v3/images/generations"
	}
	if queryEndpoint == "" {
		queryEndpoint = endpoint
	}
	return &VolcEngineImageClient{
		BaseURL:       baseURL,
		APIKey:        apiKey,
		Model:         model,
		Endpoint:      endpoint,
		QueryEndpoint: queryEndpoint,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

func (c *VolcEngineImageClient) GenerateImage(prompt string, opts ...ImageOption) (*ImageResult, error) {
	options := &ImageOptions{
		Size:    "2K",
		Quality: "standard",
	}

	for _, opt := range opts {
		opt(options)
	}

	model := c.Model
	if options.Model != "" {
		model = options.Model
	}

	promptText := prompt
	if options.NegativePrompt != "" {
		promptText += fmt.Sprintf(". Negative: %s", options.NegativePrompt)
	}

	size := options.Size
	// 将像素尺寸转换为火山引擎格式
	// 火山引擎 API 要求图片至少 3686400 像素
	// 2K = 2560x2560 = 6,553,600 pixels
	// 1K = 1024x1024 = 1,048,576 pixels (低于最低要求)
	// 2560x1440 = 3,686,400 pixels (正好是最低要求)
	// 1920x1080 = 2,073,600 pixels (低于最低要求，需要升级到 2K)
	if size == "1920x1920" || size == "2048x2048" || size == "2560x2560" {
		size = "2K"
	} else if size == "2560x1440" || size == "1440x2560" {
		// 16:9 和 9:16 的 2K 分辨率
		size = "2K"
	} else if size == "1920x1080" || size == "1080x1920" || size == "1280x720" || size == "720x1280" {
		// 1080p 和 720p 分辨率低于火山引擎最低要求，升级到 2K
		size = "2K"
	} else if size == "1024x1024" || size == "1280x1280" {
		// 1K 正方形分辨率低于最低要求，升级到 2K
		size = "2K"
	} else if size == "" {
		if model == "doubao-seedream-4-5-251128" {
			size = "2K"
		} else {
			size = "2K"
		}
	}

	reqBody := VolcEngineImageRequest{
		Model:                     model,
		Prompt:                    promptText,
		Image:                     options.ReferenceImages,
		SequentialImageGeneration: "disabled",
		Size:                      size,
		Watermark:                 false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.BaseURL + c.Endpoint
	fmt.Printf("[VolcEngine Image] Request URL: %s\n", url)
	fmt.Printf("[VolcEngine Image] Request Body: %s\n", string(jsonData))

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	fmt.Printf("VolcEngine Image API Response: %s\n", string(body))

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result VolcEngineImageResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("volcengine error: %v", result.Error)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no image generated")
	}

	return &ImageResult{
		Status:    "completed",
		ImageURL:  result.Data[0].URL,
		Completed: true,
	}, nil
}

func (c *VolcEngineImageClient) GetTaskStatus(taskID string) (*ImageResult, error) {
	return nil, fmt.Errorf("not supported for VolcEngine Seedream (synchronous generation)")
}
