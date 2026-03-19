/**
 * 图片URL工具函数
 */

/**
 * 修复图片URL，处理相对路径和绝对路径
 */
export function fixImageUrl(url: string): string {
  if (!url) return "";
  if (url.startsWith("http") || url.startsWith("data:")) return url;
  return `${import.meta.env.VITE_API_BASE_URL || ""}${url}`;
}

/**
 * 获取图片URL，优先使用 local_path 或 image_local_path
 * @param item 包含 local_path、image_local_path 或 image_url 的对象
 * @returns 处理后的图片URL
 */
export function getImageUrl(item: any): string {
  if (!item) return "";

  // 优先使用 image_local_path（图片专用本地路径）
  if (item.image_local_path) {
    if (item.image_local_path.startsWith("http")) {
      return item.image_local_path;
    }
    return `/static/${item.image_local_path}`;
  }

  // 其次使用 local_path（通用本地路径）
  if (item.local_path) {
    // local_path 是相对路径（如 images/xxx.jpg），需要添加 /static/ 前缀
    return `/static/${item.local_path}`;
  }

  // 回退到 image_url
  if (item.image_url) {
    return fixImageUrl(item.image_url);
  }

  // 回退到 composed_image_local_path（用于 storyboard 的组合图）
  if (item.composed_image_local_path) {
    return `/static/${item.composed_image_local_path}`;
  }

  // 回退到 composed_image
  if (item.composed_image) {
    return fixImageUrl(item.composed_image);
  }

  return "";
}

/**
 * 检查是否有图片
 */
export function hasImage(item: any): boolean {
  return !!(item?.local_path || item?.image_local_path || item?.image_url || item?.composed_image_local_path || item?.composed_image);
}

/**
 * 获取视频URL，优先使用 local_path 或 video_local_path
 * @param item 包含 local_path、video_local_path 或 video_url 或 url 的对象
 * @returns 处理后的视频URL
 */
export function getVideoUrl(item: any): string {
  if (!item) return "";

  // 优先使用 video_local_path（视频专用本地路径）
  if (item.video_local_path) {
    if (item.video_local_path.startsWith("http")) {
      return item.video_local_path;
    }
    return `/static/${item.video_local_path}`;
  }

  // 其次使用 local_path（通用本地路径）
  if (item.local_path) {
    // 如果 local_path 已经是完整 URL，直接返回
    if (item.local_path.startsWith("http")) {
      return item.local_path;
    }
    // 否则添加 /static/ 前缀
    return `/static/${item.local_path}`;
  }

  // 回退到 video_url
  if (item.video_url) {
    return fixImageUrl(item.video_url);
  }

  // 回退到 url（用于 assets）
  if (item.url) {
    return fixImageUrl(item.url);
  }

  return "";
}

/**
 * 检查是否有视频
 */
export function hasVideo(item: any): boolean {
  return !!(item?.local_path || item?.video_local_path || item?.video_url || item?.url);
}
