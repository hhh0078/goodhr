# GoodHR SidePanel Vue

这是一个独立的新插件目录，目标是把现有 GoodHR 插件改成 `Vue + Vite` 的侧边栏版本。

## 开发命令

```bash
npm install
npm run watch
```

`watch` 会持续把源码编译到 `goodhr-sidebar-vue/extension`。

## 加载方式

1. 打开 `Chrome` 扩展管理页。
2. 打开“开发者模式”。
3. 选择“加载已解压的扩展程序”。
4. 选择 `goodhr-sidebar-vue/extension`。
5. 点击扩展图标，侧边栏会打开。

## 当前实现

- 侧边栏外壳使用 `Vue`
- 现有 `popup/index.html + popup/index.js` 被复制到 `legacy` 目录
- 侧边栏里通过 `iframe` 承载旧版界面，先保证功能和视觉接近现状
- 后续可以逐步把 `legacy` 逻辑迁移成真正的 Vue 组件
