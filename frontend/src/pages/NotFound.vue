<script setup lang="ts">
import { ArrowLeftOutlined, SafetyCertificateOutlined } from '@ant-design/icons-vue'
import MarvoMark from '../components/MarvoMark.vue'
</script>

<template>
  <main class="not-found-page">
    <div class="not-found-card">
      <MarvoMark />
      <p class="not-found-code">404</p>
      <h1>这里没有你要找的页面</h1>
      <p>请检查访问地址，或者回到 Marvo 首页重新开始。</p>
      <div class="not-found-actions">
        <router-link class="not-found-primary" to="/"><ArrowLeftOutlined aria-hidden="true" />返回首页</router-link>
        <router-link class="not-found-secondary" to="/admin">
          <SafetyCertificateOutlined aria-hidden="true" />平台管理
        </router-link>
      </div>
    </div>
  </main>
</template>

<style scoped>
.not-found-page {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background:
    radial-gradient(circle at 50% 28%, color-mix(in srgb, #5848f5 13%, transparent), transparent 24rem), var(--bg-login);
}

.not-found-card {
  width: min(500px, 100%);
  text-align: center;
}

.not-found-card > .marvo-mark {
  width: 58px;
  height: 58px;
  color: #5848f5;
}

.not-found-code {
  margin-top: 24px;
  color: var(--text-accent);
  font-size: 0.86rem;
  font-weight: 750;
  letter-spacing: 0.14em;
}

.not-found-card h1 {
  margin-top: 8px;
  color: var(--text-primary);
  font-size: clamp(2rem, 6vw, 3rem);
  letter-spacing: -0.045em;
}

.not-found-card > p:last-of-type {
  margin-top: 14px;
  color: var(--text-tertiary);
  line-height: 1.7;
}

.not-found-actions {
  margin-top: 30px;
  display: flex;
  justify-content: center;
  gap: 10px;
}

.not-found-actions a {
  min-height: 42px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 0 15px;
  border-radius: 10px;
  text-decoration: none;
  font-weight: 650;
}

.not-found-primary {
  background: #5848f5;
  color: #fff;
}

.not-found-secondary {
  border: 1px solid var(--border-primary);
  background: var(--bg-primary);
  color: var(--text-secondary);
}

@media (max-width: 420px) {
  .not-found-actions {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
