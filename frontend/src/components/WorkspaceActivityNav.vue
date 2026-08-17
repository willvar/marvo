<script setup lang="ts">
import { BellOutlined, ClockCircleOutlined } from '@ant-design/icons-vue'
import { workspaceRoute } from '../sdk'
</script>

<template>
  <nav class="workspace-activity-nav" aria-label="活动与自动任务">
    <RouterLink :to="workspaceRoute('/activity')"><BellOutlined aria-hidden="true" />活动</RouterLink>
    <RouterLink :to="workspaceRoute('/schedules')"><ClockCircleOutlined aria-hidden="true" />自动任务</RouterLink>
  </nav>
</template>

<style lang="scss" scoped>
.workspace-activity-nav {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px;
  border-radius: 10px;
  background: var(--bg-secondary);

  a {
    min-height: 34px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    padding: 0 13px;
    border-radius: 7px;
    color: var(--text-tertiary);
    font-size: var(--marvo-type-13);
    font-weight: 500;
    text-decoration: none;
    transition:
      color 0.15s,
      background 0.15s,
      box-shadow 0.15s;

    &:hover {
      color: var(--text-primary);
    }

    &.router-link-active {
      background: var(--bg-primary);
      color: var(--text-primary);
      box-shadow: 0 1px 3px color-mix(in srgb, #000 10%, transparent);
    }
  }
}

@media (hover: none), (max-width: 768px) {
  .workspace-activity-nav a {
    min-height: 40px;
  }
}

@media (max-width: 520px) {
  .workspace-activity-nav {
    width: 100%;
  }

  .workspace-activity-nav a {
    flex: 1;
  }
}
</style>
