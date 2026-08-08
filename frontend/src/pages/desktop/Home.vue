<script setup lang="ts">
import { ref } from 'vue'
import { FileTextOutlined } from '@ant-design/icons-vue'
import { useRouter } from 'vue-router'
import AgentComposer from '../../components/AgentComposer.vue'
import { XWelcome } from '../../components/x'
import type { AgentFilePartInput } from '../../sdk'
import { useAgentStore } from '../../stores/agent'

const router = useRouter()
const agent = useAgentStore()
const launchError = ref('')

async function startAgentConversation(text: string, files: AgentFilePartInput[]) {
  launchError.value = ''
  try {
    agent.connect()
    await agent.createSession()
    await agent.sendMessage(text, files)
    await router.push('/agent')
  } catch {
    throw new Error('发送失败，智能体服务可能不可用')
  }
}
</script>

<template>
  <div class="home">
    <div class="home-start">
      <XWelcome
        class="home-welcome"
        title="开始创作"
        description="在左侧列表上方的“搜索或新建”输入框开始创作，或直接告诉智能体你想完成什么。"
        :icon="FileTextOutlined"
        variant="filled"
      />
      <AgentComposer
        class="home-agent-composer"
        placeholder="向智能体描述你想完成的内容..."
        :submit-message="startAgentConversation"
        @error="launchError = $event"
      />
      <p v-if="launchError" class="home-agent-error" role="alert">{{ launchError }}</p>
    </div>
  </div>
</template>

<style lang="scss" scoped>
.home {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  padding: 24px;
  box-sizing: border-box;
  overflow-y: auto;
}
.home-start {
  width: min(100%, 680px);
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.home-agent-error {
  margin: 0;
  color: var(--text-danger);
  font-size: var(--marvo-type-13);
  text-align: center;
}
@media (max-width: 600px) {
  .home {
    padding: 16px;
  }
}
</style>
