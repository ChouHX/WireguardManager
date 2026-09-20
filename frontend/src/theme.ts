import { createTheme, rem, type MantineColorsTuple } from '@mantine/core';

/**
 * 品牌色：WireGuard 标识的氧化红，用于主操作与强调。
 * 10 阶色板（Mantine 约定 light 模式取 6，dark 模式取 8）。
 */
const wgRed: MantineColorsTuple = [
  '#fff5f4',
  '#ffe6e3',
  '#fcc9c4',
  '#f5a79f',
  '#ee887e',
  '#e97065',
  '#e45f52',
  '#c94b3f',
  '#b44035',
  '#9a3529',
];

/** 辅助色：控制台里的数据高亮用青绿，和品牌红形成冷暧对比 */
const wgTeal: MantineColorsTuple = [
  '#e6fbf8',
  '#d0f5f0',
  '#a3e9e2',
  '#72dcd2',
  '#4bd1c5',
  '#33cabe',
  '#26c7ba',
  '#14afa4',
  '#009c91',
  '#00877d',
];

export const theme = createTheme({
  primaryColor: 'wg',
  primaryShade: { light: 6, dark: 8 },
  colors: { wg: wgRed, teal: wgTeal },

  fontFamily:
    "'IBM Plex Sans', 'Noto Sans SC', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', system-ui, sans-serif",
  fontFamilyMonospace:
    "'JetBrains Mono', 'IBM Plex Mono', 'SFMono-Regular', Menlo, Consolas, 'Liberation Mono', monospace",

  headings: {
    fontFamily:
      "'IBM Plex Sans', 'Noto Sans SC', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', system-ui, sans-serif",
    fontWeight: '650',
    sizes: {
      h1: { fontSize: rem(30), lineHeight: '1.25' },
      h2: { fontSize: rem(24), lineHeight: '1.3' },
      h3: { fontSize: rem(19), lineHeight: '1.35' },
      h4: { fontSize: rem(16), lineHeight: '1.4' },
    },
  },

  defaultRadius: 'xs',
  cursorType: 'pointer',
  focusRing: 'auto',
  scale: 1,

  // 微圆角：整体趋近直角，只保留 1-6px 的轻微倒角。
  // 需要正圆的元素（头像、圆形按钮）显式使用 radius="50%"。
  radius: {
    xs: '1px',
    sm: '2px',
    md: '3px',
    lg: '4px',
    xl: '6px',
  },

  // 紧凑式布局：整体间距刻度比 Mantine 默认值收紧约 25%，
  // 使页面在同样屏幕高度下能容纳更多信息，同时保留可读的呼吸感。
  spacing: {
    xs: '6px',
    sm: '9px',
    md: '12px',
    lg: '16px',
    xl: '22px',
  },

  other: {
    /** 侧边栏在两种配色方案下都保持深色控制台质感 */
    navBackground: '#16181d',
    navBorder: '#262b34',
    navText: '#a9b1bd',
    navTextActive: '#ffffff',
    navActiveBg: 'rgba(228, 95, 82, 0.16)',
  },

  components: {
    Card: {
      defaultProps: { withBorder: true, radius: 'xs', padding: 'md' },
    },
    Paper: {
      defaultProps: { radius: 'xs' },
    },
    Button: {
      defaultProps: { radius: 'xs' },
    },
    ActionIcon: {
      defaultProps: { radius: 'xs' },
    },
    Badge: {
      defaultProps: { radius: 'xs', variant: 'light' },
    },
    Modal: {
      defaultProps: { radius: 'sm', centered: true, overlayProps: { blur: 3 } },
    },
    Drawer: {
      defaultProps: { radius: 'sm' },
    },
    Menu: {
      defaultProps: { radius: 'sm' },
    },
    Table: {
      defaultProps: { highlightOnHover: true, verticalSpacing: 'xs', horizontalSpacing: 'sm' },
    },
    TextInput: {
      defaultProps: { radius: 'xs' },
    },
    PasswordInput: {
      defaultProps: { radius: 'xs' },
    },
    NumberInput: {
      defaultProps: { radius: 'xs' },
    },
    TagsInput: {
      defaultProps: { radius: 'xs' },
    },
    Select: {
      defaultProps: { radius: 'xs' },
    },
    Alert: {
      defaultProps: { radius: 'xs' },
    },
    Tooltip: {
      defaultProps: { withArrow: true, openDelay: 200, radius: 'xs' },
    },
    Notification: {
      defaultProps: { radius: 'sm' },
    },
  },
});
