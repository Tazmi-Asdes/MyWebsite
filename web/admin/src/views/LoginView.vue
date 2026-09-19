<script setup lang="ts">
function submitLogin(): void {
  // Stage 0 is a visual and routing scaffold; authentication is intentionally deferred.
}
</script>

<template>
  <main class="login-page">
    <section class="login-panel" aria-labelledby="login-title">
      <p class="eyebrow">个人技术网站 / 管理端</p>
      <h1 id="login-title">管理员登录</h1>
      <p class="login-intro">登录后管理文章与公开项目。</p>

      <form class="form-stack" @submit.prevent="submitLogin">
        <div class="field">
          <label for="username">用户名</label>
          <input
            id="username"
            name="username"
            type="text"
            autocomplete="username"
            required
            autofocus
          />
        </div>

        <div class="field">
          <label for="password">密码</label>
          <input
            id="password"
            name="password"
            type="password"
            autocomplete="current-password"
            required
          />
        </div>

        <button class="button button--primary" type="submit">登录</button>
      </form>
      <p class="stage-note">Stage 0 占位页面，认证功能将在后续阶段接入。</p>
    </section>
  </main>
</template>
