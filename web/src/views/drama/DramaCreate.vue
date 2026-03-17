<template>
  <!-- Drama Create Page / 创建短剧页面 -->
  <div class="page-container">
    <div class="content-wrapper animate-fade-in">
      <!-- Header / 头部 -->
      <AppHeader :fixed="false" :show-logo="false">
        <template #left>
          <el-button text @click="goBack" class="back-btn">
            <el-icon><ArrowLeft /></el-icon>
            <span>返回</span>
          </el-button>
          <div class="page-title">
            <h1>创建新项目</h1>
            <span class="subtitle">填写基本信息来创建你的短剧项目</span>
          </div>
        </template>
      </AppHeader>

      <!-- Form Card / 表单卡片 -->
      <div class="form-card">

        <el-form
          ref="formRef"
          :model="form"
          :rules="rules"
          label-position="top"
          class="create-form"
          @submit.prevent="handleSubmit"
        >
          <el-form-item label="项目标题" prop="title" required>
            <el-input
              v-model="form.title"
              placeholder="给你的短剧起个名字"
              size="large"
              maxlength="100"
              show-word-limit
            />
          </el-form-item>

          <el-form-item label="项目描述" prop="description">
            <el-input
              v-model="form.description"
              type="textarea"
              :rows="5"
              placeholder="简要描述你的短剧内容、风格或创意（可选）"
              maxlength="500"
              show-word-limit
              resize="none"
            />
          </el-form-item>

          <!-- 分辨率配置区域 -->
          <div class="resolution-config">
            <h3 class="config-title">图片与视频配置</h3>

            <el-row :gutter="16">
              <el-col :span="12">
                <el-form-item label="图片长宽比" prop="image_aspect_ratio">
                  <el-select v-model="form.image_aspect_ratio" size="large" style="width: 100%" @change="onAspectRatioChange">
                    <el-option label="16:9 横屏" value="16:9" />
                    <el-option label="9:16 竖屏" value="9:16" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="图片分辨率" prop="image_resolution">
                  <el-select v-model="form.image_resolution" size="large" style="width: 100%">
                    <el-option
                      v-for="option in imageResolutionOptions"
                      :key="option.value"
                      :label="option.label"
                      :value="option.value"
                    />
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>

            <el-form-item label="视频分辨率" prop="video_resolution">
              <el-radio-group v-model="form.video_resolution" size="large">
                <el-radio-button label="720p" />
                <el-radio-button label="1080p" />
              </el-radio-group>
              <div class="resolution-tip">
                720p: 1280x720 | 1080p: 1920x1080
              </div>
            </el-form-item>
          </div>

          <!-- 风格配置 -->
          <el-form-item label="画风风格" prop="style">
            <el-select v-model="form.style" size="large" placeholder="选择画风风格（可选）" clearable style="width: 100%">
              <el-option label="写实风格" value="realistic" />
              <el-option label="吉卜力风格" value="ghibli" />
              <el-option label="动漫风格" value="anime" />
              <el-option label="水彩风格" value="watercolor" />
              <el-option label="油画风格" value="oil_painting" />
              <el-option label="赛博朋克" value="cyberpunk" />
              <el-option label="像素风格" value="pixel" />
            </el-select>
          </el-form-item>

          <div class="form-actions">
            <el-button size="large" @click="goBack">取消</el-button>
            <el-button
              type="primary"
              size="large"
              :loading="loading"
              @click="handleSubmit"
            >
              <el-icon v-if="!loading"><Plus /></el-icon>
              创建项目
            </el-button>
          </div>
        </el-form>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { ArrowLeft, Plus } from '@element-plus/icons-vue'
import { dramaAPI } from '@/api/drama'
import type { CreateDramaRequest } from '@/types/drama'
import { AppHeader } from '@/components/common'

const router = useRouter()
const formRef = ref<FormInstance>()
const loading = ref(false)

const form = reactive<CreateDramaRequest>({
  title: '',
  description: '',
  style: '',
  image_aspect_ratio: '16:9',
  image_resolution: '2560x1440',
  video_resolution: '1080p'
})

// 根据长宽比动态生成分辨率选项
const imageResolutionOptions = computed(() => {
  if (form.image_aspect_ratio === '16:9') {
    return [
      { label: '2560 x 1440 (2K)', value: '2560x1440' },
      { label: '1920 x 1080 (1080p)', value: '1920x1080' },
      { label: '1280 x 720 (720p)', value: '1280x720' }
    ]
  } else {
    return [
      { label: '1440 x 2560 (2K 竖屏)', value: '1440x2560' },
      { label: '1080 x 1920 (1080p 竖屏)', value: '1080x1920' },
      { label: '720 x 1280 (720p 竖屏)', value: '720x1280' }
    ]
  }
})

// 长宽比变化时，自动切换到对应的默认分辨率
const onAspectRatioChange = () => {
  if (form.image_aspect_ratio === '16:9') {
    form.image_resolution = '2560x1440'
  } else {
    form.image_resolution = '1440x2560'
  }
}

const rules: FormRules = {
  title: [
    { required: true, message: '请输入项目标题', trigger: 'blur' },
    { min: 1, max: 100, message: '标题长度在 1 到 100 个字符', trigger: 'blur' }
  ]
}

// Submit form / 提交表单
const handleSubmit = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async (valid) => {
    if (valid) {
      loading.value = true
      try {
        const drama = await dramaAPI.create(form)
        ElMessage.success('创建成功')
        router.push(`/dramas/${drama.id}`)
      } catch (error: any) {
        ElMessage.error(error.message || '创建失败')
      } finally {
        loading.value = false
      }
    }
  })
}

// Go back / 返回上一页
const goBack = () => {
  router.back()
}
</script>

<style scoped>
/* ========================================
   Page Layout / 页面布局 - 紧凑边距
   ======================================== */
.page-container {
  min-height: 100vh;
  background-color: var(--bg-primary);
  padding: var(--space-2) var(--space-3);
  transition: background-color var(--transition-normal);
}

@media (min-width: 768px) {
  .page-container {
    padding: var(--space-3) var(--space-4);
  }
}

.content-wrapper {
  max-width: 640px;
  margin: 0 auto;
}

/* ========================================
   Form Card / 表单卡片
   ======================================== */
.form-card {
  background: var(--bg-card);
  border: 1px solid var(--border-primary);
  border-radius: var(--radius-xl);
  overflow: hidden;
  box-shadow: var(--shadow-card);
}

/* ========================================
   Form Styles / 表单样式 - 紧凑内边距
   ======================================== */
.create-form {
  padding: var(--space-4);
}

.create-form :deep(.el-form-item) {
  margin-bottom: var(--space-4);
}

/* ========================================
   Form Actions / 表单操作区
   ======================================== */
.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-3);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border-primary);
  margin-top: var(--space-2);
}

.form-actions .el-button {
  min-width: 100px;
}
</style>
