import type {
  GenerateVideoRequest,
  VideoGeneration,
  VideoGenerationListParams
} from '../types/video'
import request from '../utils/request'

export const videoAPI = {
  generateVideo(data: GenerateVideoRequest) {
    return request.post<VideoGeneration>('/videos', data)
  },

  generateFromImage(imageGenId: number) {
    return request.post<VideoGeneration>(`/videos/image/${imageGenId}`)
  },

  batchGenerateForEpisode(episodeId: number) {
    return request.post<VideoGeneration[]>(`/videos/episode/${episodeId}/batch`)
  },

  getVideoGeneration(id: number) {
    return request.get<VideoGeneration>(`/videos/${id}`)
  },
  
  getVideo(id: number) {
    return request.get<VideoGeneration>(`/videos/${id}`)
  },

  listVideos(params: VideoGenerationListParams) {
    return request.get<{
      items: VideoGeneration[]
      pagination: {
        page: number
        page_size: number
        total: number
        total_pages: number
      }
    }>('/videos', { params })
  },

  deleteVideo(id: number) {
    return request.delete(`/videos/${id}`)
  },

  generateKeyframeVideoPrompts(data: {
    storyboard_id: number;
    frame_image_ids: number[];
    generation_mode?: string;
  }): Promise<{ prompts: string[]; durations?: number[] }> {
    return request.post<{ prompts: string[]; durations?: number[] }>('/videos/keyframe-prompts', data)
  },

  generateKeyframeSequenceVideos(data: {
    storyboard_id: number;
    drama_id: string;
    frame_image_ids: number[];
    video_prompts: string[];
    generation_mode?: 'parallel' | 'sequential' | 'keyframe_parallel';
    model?: string;
    durations?: number[];
  }): Promise<{ task_id: string; status: string }> {
    return request.post<{ task_id: string; status: string }>('/videos/keyframe-sequence', data)
  }
}
