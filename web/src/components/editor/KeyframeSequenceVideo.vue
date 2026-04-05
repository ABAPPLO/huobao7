<template>
  <div
    class="keyframe-video-section"
    style="margin-top: 16px; padding: 16px; background: #f5f7f33; border-radius: 8px"
  >
    <div
      class="section-title"
      style="font-weight: bold; margin-bottom: 12px; display: flex; align-items: center; gap: 12px"
    >
      <span>关键帧序列视频生成</span>
      <el-tag type="info" size="small">
        {{ actionSequenceImages.length }}帧 / {{ actionSequenceImages.length - 1 }}段视频
      </el-tag>
      <el-radio-group v-model="keyframeFrameCount" size="small" style="margin-left: auto">
        <el-radio-button :value="4">4帧</el-radio-button>
        <el-radio-button :value="6">6帧</el-radio-button>
        <el-radio-button :value="9">9帧</el-radio-button>
        <el-radio-button :value="0">全部({{ availableImages.length }})</el-radio-button>
      </el-radio-group>
    </div>

    <!-- 生成方式选择 -->
    <div class="generation-mode-selector" style="margin-bottom: 12px">
      <span style="margin-right: 12px">生成方式:</span>
      <el-radio-group v-model="keyframeGenerationMode" size="small">
        <el-radio-button value="parallel">并行生成</el-radio-button>
        <el-radio-button value="sequential">串行生成</el-radio-button>
      </el-radio-group>
    </div>

    <!-- 帧插槽 -->
    <div class="keyframe-frame-slots" style="margin-bottom: 16px">
      <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 8px">
        <span style="font-size: 13px; color: #606266; font-weight: 500">帧分配</span>
        <el-tag type="info" size="small">
          {{ keyframeFrameSlots.filter(id => id !== null).length }}/{{ keyframeFrameSlots.length }}
          已分配
        </el-tag>
        <el-button size="small" text @click="autoFillKeyframeSlots" style="margin-left: auto">
          自动填充
        </el-button>
      </div>
      <div
        style="display: flex; gap: 8px; flex-wrap: wrap; justify-content: center; align-items: center"
      >
        <template v-for="(slotImageId, index) in keyframeFrameSlots" :key="index">
          <div
            class="frame-slot"
            :style="{ width: slotWidth + 'px', height: slotHeight + 'px' }"
            :class="{ active: activeKeyframeSlot === index }"
            @click="activeKeyframeSlot = activeKeyframeSlot === index ? -1 : index"
          >
            <div class="frame-slot-label">帧{{ index + 1 }}</div>
            <div class="frame-slot-image" v-if="getKeyframeSlotImage(index)">
              <img
                :src="getImageUrl(getKeyframeSlotImage(index)!)"
                style="width: 100%; height: 100%; object-fit: cover"
              />
              <div class="frame-slot-remove" @click.stop="clearKeyframeSlot(index)">
                <el-icon :size="12" color="#fff"><Close /></el-icon>
              </div>
            </div>
            <div v-else class="frame-slot-placeholder">
              <el-icon :size="18" color="#c0c4cc"><Plus /></el-icon>
            </div>
          </div>
          <el-icon
            v-if="index < keyframeFrameSlots.length - 1"
            :size="14"
            color="#909399"
            style="flex-shrink: 0"
          >
            <Right />
          </el-icon>
        </template>
      </div>

      <!-- 图片选择器：激活插槽后显示可用图片 -->
      <div
        v-if="activeKeyframeSlot >= 0"
        class="frame-image-picker"
        style="margin-top: 10px; padding: 10px; background: #fff; border: 1px dashed #dcdfe6; border-radius: 6px"
      >
        <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 8px">
          <span style="font-size: 12px; color: #909399">
            点击下方图片分配到 <el-tag size="small" type="primary">帧{{ activeKeyframeSlot + 1 }}</el-tag>
          </span>
          <el-button size="small" text @click="activeKeyframeSlot = -1" style="margin-left: auto">
            取消选择
          </el-button>
        </div>
        <div v-if="availableImages.length === 0" style="text-align: center; color: #c0c4cc; padding: 16px">
          暂无可用图片
        </div>
        <div v-else style="display: flex; gap: 6px; flex-wrap: wrap; max-height: 200px; overflow-y: auto">
          <div
            v-for="img in availableImages"
            :key="img.id"
            class="picker-image-item"
            :class="{ selected: keyframeFrameSlots[activeKeyframeSlot] === img.id }"
            @click="assignImageToSlot(img.id)"
          >
            <el-image
              :src="getImageUrl(img)"
              fit="cover"
              :style="{ width: thumbWidth + 'px', height: thumbHeight + 'px', borderRadius: '4px' }"
            />
            <div v-if="img.frame_type" class="picker-image-type">
              {{ frameTypeLabel(img.frame_type) }}
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 视频提示词列表 -->
    <div class="video-prompts-list" style="max-height: 300px; overflow-y: auto">
      <div
        v-for="(prompt, index) in keyframeVideoPrompts"
        :key="index"
        class="prompt-item"
        style="margin-bottom: 12px; padding: 12px; background: #fff; border-radius: 4px"
      >
        <div class="prompt-header" style="display: flex; align-items: center; margin-bottom: 8px">
          <el-tag size="small" type="primary">帧{{ index + 1 }} → 帧{{ index + 2 }}</el-tag>
          <span style="margin-left: 8px; font-size: 12px; color: #666">视频提示词</span>
        </div>
        <el-input
          v-model="keyframeVideoPrompts[index]"
          type="textarea"
          :rows="2"
          placeholder="描述从帧{{ index + 1 }}到帧{{ index + 2 }}的动作过渡..."
        />
      </div>
    </div>

    <!-- 操作按钮 -->
    <div class="action-buttons" style="margin-top: 12px; display: flex; gap: 8px">
      <el-button
        @click="handleGeneratePrompts"
        :loading="generatingKeyframePrompts"
        :disabled="generatingKeyframeVideos"
      >
        <el-icon><MagicStick /></el-icon>
        生成视频提示词
      </el-button>
      <el-button
        type="primary"
        @click="handleStartGeneration"
        :loading="generatingKeyframeVideos"
        :disabled="generatingKeyframePrompts || keyframeVideoPrompts.length === 0"
      >
        <el-icon><VideoCamera /></el-icon>
        生成视频 ({{ keyframeVideoPrompts.length }}段)
      </el-button>
    </div>

    <!-- 生成进度 -->
    <div
      v-if="generatingKeyframeVideos && keyframeVideoTaskId"
      class="progress-info"
      style="margin-top: 12px; padding: 12px; background: #e6f7ff; border-radius: 4px; text-align: center"
    >
      <el-progress
        :percentage="keyframeVideoProgress"
        :status="keyframeVideoProgress === 100 ? 'success' : ''"
      />
      <span style="margin-top: 8px">{{ keyframeVideoStatus }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from "vue";
import { Close, MagicStick, Plus, Right, VideoCamera } from "@element-plus/icons-vue";
import { ElMessage } from "element-plus";
import { videoAPI } from "@/api/video";
import { taskAPI } from "@/api/task";
import { getImageUrl } from "@/utils/image";
import type { ImageGeneration } from "@/types/image";

interface Props {
  storyboardId: number;
  dramaId: number;
  availableImages: ImageGeneration[];
  selectedVideoModel: string;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  (e: "videos-generated"): void;
}>();

// Generation state
const keyframeGenerationMode = ref<"parallel" | "sequential">("parallel");
const keyframeVideoPrompts = ref<string[]>([]);
const generatingKeyframePrompts = ref(false);
const generatingKeyframeVideos = ref(false);
const keyframeVideoTaskId = ref<string | null>(null);
const keyframeVideoProgress = ref(0);
const keyframeVideoStatus = ref("");

// 从实际图片尺寸计算宽高比，用于帧插槽和选择器的尺寸适配
const imageAspectRatio = computed(() => {
  const img = props.availableImages.find((i) => i.width && i.height);
  if (img && img.width && img.height) {
    return `${img.width}/${img.height}`;
  }
  return "16/9"; // 默认
});

// 根据宽高比计算插槽高度（宽度固定80px）
const slotWidth = 80;
const slotHeight = computed(() => {
  const parts = imageAspectRatio.value.split("/");
  if (parts.length === 2) {
    const w = Number(parts[0]);
    const h = Number(parts[1]);
    if (w > 0 && h > 0) return Math.round(slotWidth * h / w);
  }
  return 45;
});

// 选择器缩略图尺寸
const thumbWidth = 72;
const thumbHeight = computed(() => {
  const parts = imageAspectRatio.value.split("/");
  if (parts.length === 2) {
    const w = Number(parts[0]);
    const h = Number(parts[1]);
    if (w > 0 && h > 0) return Math.round(thumbWidth * h / w);
  }
  return 40;
});

// Frame count & slots
const keyframeFrameCount = ref<number>(0); // 0 = all
const actionSequenceImages = computed(() => {
  if (keyframeFrameCount.value === 0) return props.availableImages;
  return props.availableImages.slice(0, keyframeFrameCount.value);
});

const keyframeFrameSlots = ref<(number | null)[]>([]);
const activeKeyframeSlot = ref<number>(-1);

// Auto-fill frame slots
const autoFillKeyframeSlots = () => {
  const count =
    keyframeFrameCount.value === 0 ? props.availableImages.length : keyframeFrameCount.value;
  keyframeFrameSlots.value = actionSequenceImages.value.slice(0, count).map((img) => img.id);
  activeKeyframeSlot.value = -1;
};

// Watch frame count and images changes to auto-fill
watch(
  [keyframeFrameCount, () => props.availableImages],
  () => {
    const count =
      keyframeFrameCount.value === 0 ? props.availableImages.length : keyframeFrameCount.value;
    const newSlots: (number | null)[] = [];
    for (let i = 0; i < count; i++) {
      if (i < keyframeFrameSlots.value.length && keyframeFrameSlots.value[i] !== null) {
        const existingId = keyframeFrameSlots.value[i];
        if (props.availableImages.some((img) => img.id === existingId)) {
          newSlots.push(existingId);
          continue;
        }
      }
      if (i < actionSequenceImages.value.length) {
        newSlots.push(actionSequenceImages.value[i].id);
      } else {
        newSlots.push(null);
      }
    }
    keyframeFrameSlots.value = newSlots;
  },
  { immediate: true },
);

// Get image object for a slot
const getKeyframeSlotImage = (index: number): ImageGeneration | null => {
  const imageId = keyframeFrameSlots.value[index];
  if (imageId === null || imageId === undefined) return null;
  return props.availableImages.find((img) => img.id === imageId) || null;
};

// Clear a slot
const clearKeyframeSlot = (index: number) => {
  keyframeFrameSlots.value[index] = null;
  activeKeyframeSlot.value = -1;
};

// Assign an image to the active slot (from picker)
const assignImageToSlot = (imageId: number) => {
  if (activeKeyframeSlot.value < 0) return;
  const existingSlotIndex = keyframeFrameSlots.value.indexOf(imageId);
  const currentSlotValue = keyframeFrameSlots.value[activeKeyframeSlot.value];
  if (existingSlotIndex >= 0 && existingSlotIndex !== activeKeyframeSlot.value) {
    keyframeFrameSlots.value[existingSlotIndex] = currentSlotValue;
  }
  keyframeFrameSlots.value[activeKeyframeSlot.value] = imageId;
  activeKeyframeSlot.value = -1;
};

// Frame type display label
const frameTypeLabel = (type: string): string => {
  const map: Record<string, string> = {
    first: '首帧',
    last: '尾帧',
    key: '关键帧',
    panel: '分镜板',
    action: '动作序列',
  };
  return map[type] || type;
};

// Handle image click from parent (assign to active slot)
const handleImageClick = (imageId: number): boolean => {
  if (activeKeyframeSlot.value < 0) return false;
  const existingSlotIndex = keyframeFrameSlots.value.indexOf(imageId);
  const currentSlotValue = keyframeFrameSlots.value[activeKeyframeSlot.value];
  if (existingSlotIndex >= 0 && existingSlotIndex !== activeKeyframeSlot.value) {
    keyframeFrameSlots.value[existingSlotIndex] = currentSlotValue;
  }
  keyframeFrameSlots.value[activeKeyframeSlot.value] = imageId;
  activeKeyframeSlot.value = -1;
  return true;
};

// Generate video prompts via AI
const handleGeneratePrompts = async () => {
  const frameImageIds = keyframeFrameSlots.value.filter((id) => id !== null) as number[];

  if (frameImageIds.length < 2) {
    ElMessage.warning("请至少分配2帧图片到帧插槽中");
    return;
  }

  generatingKeyframePrompts.value = true;
  try {
    const result = await videoAPI.generateKeyframeVideoPrompts({
      storyboard_id: props.storyboardId,
      frame_image_ids: frameImageIds,
    });
    keyframeVideoPrompts.value = result.prompts || [];
    ElMessage.success("视频提示词生成成功");
  } catch (error: any) {
    ElMessage.error(error.message || "生成视频提示词失败");
  } finally {
    generatingKeyframePrompts.value = false;
  }
};

// Start keyframe sequence video generation
const handleStartGeneration = async () => {
  if (keyframeVideoPrompts.value.length === 0) {
    ElMessage.warning("请先生成视频提示词");
    return;
  }

  generatingKeyframeVideos.value = true;
  keyframeVideoProgress.value = 0;
  keyframeVideoStatus.value = "正在启动...";

  try {
    const frameImageIds = keyframeFrameSlots.value.filter((id) => id !== null) as number[];
    if (frameImageIds.length < 2) {
      ElMessage.warning("请至少分配2帧图片到帧插槽中");
      generatingKeyframeVideos.value = false;
      return;
    }

    const result = await videoAPI.generateKeyframeSequenceVideos({
      storyboard_id: props.storyboardId,
      drama_id: String(props.dramaId),
      frame_image_ids: frameImageIds,
      video_prompts: keyframeVideoPrompts.value,
      generation_mode: keyframeGenerationMode.value,
      model: props.selectedVideoModel || undefined,
    });

    const taskId = result.task_id;
    keyframeVideoTaskId.value = taskId;
    keyframeVideoStatus.value = "视频生成中...";

    // Poll task status
    const pollInterval = setInterval(async () => {
      try {
        const task = await taskAPI.getStatus(taskId);
        if (task.status === "completed") {
          clearInterval(pollInterval);
          generatingKeyframeVideos.value = false;
          keyframeVideoProgress.value = 100;
          keyframeVideoStatus.value = "生成完成";
          ElMessage.success("关键帧序列视频生成完成");
          emit("videos-generated");
        } else if (task.status === "failed") {
          clearInterval(pollInterval);
          generatingKeyframeVideos.value = false;
          keyframeVideoStatus.value = "";
          ElMessage.error(task.error || "生成失败");
        } else {
          if (task.message) keyframeVideoStatus.value = task.message;
          if (task.progress > 0) keyframeVideoProgress.value = task.progress;
        }
      } catch (e) {
        console.error("轮询任务状态失败:", e);
      }
    }, 3000);
  } catch (error: any) {
    generatingKeyframeVideos.value = false;
    keyframeVideoStatus.value = "";
    ElMessage.error(error.message || "启动视频生成失败");
  }
};

// Expose handleImageClick for parent to call when action images are clicked
defineExpose({ handleImageClick });
</script>

<style scoped>
.frame-slot {
  border: 2px dashed #dcdfe6;
  border-radius: 6px;
  cursor: pointer;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  position: relative;
  transition: border-color 0.2s;
  background: #fff;
}
.frame-slot:hover {
  border-color: #409eff;
}
.frame-slot.active {
  border-color: #409eff;
  box-shadow: 0 0 0 2px rgba(64, 158, 255, 0.2);
}
.frame-slot-label {
  font-size: 10px;
  color: #909399;
  margin-bottom: 2px;
}
.frame-slot-image {
  position: absolute;
  inset: 2px;
  border-radius: 4px;
  overflow: hidden;
}
.frame-slot-remove {
  position: absolute;
  top: 2px;
  right: 2px;
  width: 16px;
  height: 16px;
  background: rgba(0, 0, 0, 0.5);
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.frame-slot-placeholder {
  display: flex;
  align-items: center;
  justify-content: center;
}
.picker-image-item {
  position: relative;
  cursor: pointer;
  border: 2px solid transparent;
  border-radius: 6px;
  padding: 2px;
  transition: border-color 0.2s;
}
.picker-image-item:hover {
  border-color: #409eff;
}
.picker-image-item.selected {
  border-color: #e6a23c;
  background: #fdf6ec;
}
.picker-image-type {
  font-size: 10px;
  color: #909399;
  text-align: center;
  margin-top: 2px;
  line-height: 1;
}
</style>
