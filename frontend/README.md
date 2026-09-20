# WireGuard Manager · Web

React 19 + Vite + MantineUI 实现的管理控制台，替代原 Next.js + shadcn/ui 前端。

## 技术栈

| 领域 | 选型 |
| --- | --- |
| 构建 | Vite 8 + TypeScript 5.9 |
| UI | Mantine 8（`@mantine/core`、`@mantine/hooks`、`@mantine/notifications`、`@mantine/form`） |
| 路由 | react-router-dom 7（BrowserRouter + SPA 回退） |
| 状态 | zustand 5（仅认证状态持久化） |
| 图表 | recharts 3 |
| 图标 | `@tabler/icons-react` |
| 请求 | axios（统一拦截器 + 归一化错误） |
| 二维码 | qrcode.react |

## 目录结构

```
src/
├── components/
│   ├── charts/      图表 tooltip 等图表通用件
│   ├── common/      PageHeader / MetricCard / AuthGuard / Feedback / LoadingScreen
│   └── layout/      AppLayout(AppShell) / SideNav / AuthLayout / UserMenu
├── hooks/           通用 hooks（use-interval 等）
├── i18n/            自研轻量 i18n（Context + hook，locales/zh.json、en.json）
├── lib/             format（字节/速率/时间/负载等级）、ip-validator、navigation
├── pages/           路由页面
├── services/        api（axios 实例）、auth、admin、wireguard、monitoring
├── stores/          auth-store（zustand + persist）
├── types/           与后端 models 对齐的类型
├── styles/          global.css（网格纹理、侧边栏、动画）
├── theme.ts         Mantine 主题（品牌色 wg 色板、字体、组件默认值）
└── main.tsx         应用入口
```

## 本地开发

```bash
npm install
npm run dev          # http://localhost:3000，/api 与 /health 自动代理到后端
```

后端地址默认 `http://localhost:8080`，可用环境变量覆盖：

```bash
VITE_API_PROXY_TARGET=http://192.168.1.10:8080 npm run dev
```

## 构建与校验

```bash
npm run typecheck    # tsc --noEmit
npm run build        # 产物输出到 dist/
npm run preview      # 本地预览构建产物
```

## 环境变量

| 变量 | 作用域 | 说明 |
| --- | --- | --- |
| `VITE_API_BASE_URL` | 浏览器运行时 | 接口基地址，留空表示同源（生产由 Nginx 反代 `/api`） |
| `VITE_API_PROXY_TARGET` | 开发服务器 | Vite dev proxy 的目标后端地址 |
| `BACKEND_UPSTREAM` | 容器运行时 | Nginx 反代的后端地址，由 docker-compose 注入 |

## 容器部署

前端镜像为两阶段构建：Node 构建静态产物 → Nginx 托管并反代后端。

```bash
# 在仓库根目录
docker compose build frontend
docker compose up -d frontend
# 访问 http://<host>:3000
```

Nginx 配置位于 `nginx/default.conf.template`，其中 `${BACKEND_UPSTREAM}` 在容器启动时由官方镜像的 envsubst 机制注入。

## 设计约定

- 品牌色为 WireGuard 标识的氧化红（Mantine 主题 `wg` 色板），数据高亮使用 `teal`，状态色沿用 Mantine 语义色。
- 侧边栏在明暗两种配色下都保持深色控制台质感；主内容区背景带网格纹理，避免大面积纯色。
- 首屏元素统一用 `.wm-rise` 做一次性错峰浮现；数值/密钥/ID 统一加 `.wm-mono` 等宽字体避免抖动。
- 所有文案走 i18n，`zh.json` 与 `en.json` 键必须保持一致。
