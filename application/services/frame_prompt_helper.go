package services

import (
	"encoding/json"
	"regexp"
	"strings"
)

// parseFramePromptJSON 解析AI返回的JSON格式提示词
func (s *FramePromptService) parseFramePromptJSON(aiResponse string) *SingleFramePrompt {
	// 清理可能的markdown代码块标记
	cleaned := strings.TrimSpace(aiResponse)

	// 移除 ```json 和 ``` 标记
	re := regexp.MustCompile("(?s)```json\\s*(.+?)\\s*```")
	if matches := re.FindStringSubmatch(cleaned); len(matches) > 1 {
		cleaned = strings.TrimSpace(matches[1])
	} else {
		// 移除单独的 ``` 标记
		cleaned = strings.Trim(cleaned, "`")
		cleaned = strings.TrimSpace(cleaned)
	}

	// 尝试解析JSON
	var result SingleFramePrompt
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		s.log.Warnw("Failed to parse JSON", "error", err, "cleaned_response", cleaned)
		return nil
	}

	// 验证必需字段
	if result.Prompt == "" {
		s.log.Warnw("Parsed JSON missing prompt field", "response", cleaned)
		return nil
	}

	return &result
}

// parseActionSequenceJSON 解析AI返回的动作序列JSON（9个独立帧提示词）
func (s *FramePromptService) parseActionSequenceJSON(aiResponse string) []SingleFramePrompt {
	cleaned := strings.TrimSpace(aiResponse)

	re := regexp.MustCompile("(?s)```json\\s*(.+?)\\s*```")
	if matches := re.FindStringSubmatch(cleaned); len(matches) > 1 {
		cleaned = strings.TrimSpace(matches[1])
	} else {
		cleaned = strings.Trim(cleaned, "`")
		cleaned = strings.TrimSpace(cleaned)
	}

	var result struct {
		Frames []SingleFramePrompt `json:"frames"`
	}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		s.log.Warnw("Failed to parse action sequence JSON", "error", err, "cleaned_response", cleaned)
		return nil
	}

	if len(result.Frames) == 0 {
		s.log.Warnw("Parsed action sequence JSON has no frames", "response", cleaned)
		return nil
	}

	return result.Frames
}
