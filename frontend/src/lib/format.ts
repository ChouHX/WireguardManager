// 展示层格式化工具

/** 字节数格式化：1024 进制，保留 2 位小数 */
export function formatBytes(bytes: number, decimals = 2): string {
  if (!bytes || bytes <= 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1);
  return `${(bytes / k ** i).toFixed(decimals)} ${sizes[i]}`;
}

/** 速率格式化：输入 bytes/s */
export function formatSpeed(bytesPerSecond: number): string {
  return `${formatBytes(bytesPerSecond)}/s`;
}

/** 运行时长格式化，单位文案由调用方传入 */
export function formatUptime(
  seconds: number,
  units: { day: string; hour: string; minute: string } = { day: 'd', hour: 'h', minute: 'm' },
): string {
  const safe = Math.max(0, Math.floor(seconds));
  const days = Math.floor(safe / 86400);
  const hours = Math.floor((safe % 86400) / 3600);
  const minutes = Math.floor((safe % 3600) / 60);

  if (days > 0) return `${days}${units.day} ${hours}${units.hour}`;
  if (hours > 0) return `${hours}${units.hour} ${minutes}${units.minute}`;
  return `${minutes}${units.minute}`;
}

/** 相对时间：刚刚 / n 分钟前 / n 小时前 / n 天前 */
export function formatRelativeTime(
  iso: string | undefined,
  labels: { never: string; justNow: string; minutes: string; hours: string; days: string },
): string {
  if (!iso) return labels.never;

  const timestamp = new Date(iso).getTime();
  if (Number.isNaN(timestamp) || timestamp <= 0) return labels.never;

  const diffMinutes = Math.floor((Date.now() - timestamp) / 60000);
  if (diffMinutes < 1) return labels.justNow;
  if (diffMinutes < 60) return `${diffMinutes} ${labels.minutes}`;

  const diffHours = Math.floor(diffMinutes / 60);
  if (diffHours < 24) return `${diffHours} ${labels.hours}`;

  const diffDays = Math.floor(diffHours / 24);
  return `${diffDays} ${labels.days}`;
}

/** 本地化时间显示 */
export function formatTime(iso: string | Date, locale = 'zh-CN'): string {
  const date = iso instanceof Date ? iso : new Date(iso);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

/** 本地化日期+时间显示 */
export function formatDateTime(iso: string | Date, locale = 'zh-CN'): string {
  const date = iso instanceof Date ? iso : new Date(iso);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString(locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

/** 百分比显示 */
export function formatPercent(value: number, digits = 1): string {
  return `${(value ?? 0).toFixed(digits)}%`;
}

/** 负载等级：用于配色（>=90 危险，>=70 警告） */
export type LoadLevel = 'ok' | 'warn' | 'critical';

export function loadLevel(percent: number): LoadLevel {
  if (percent >= 90) return 'critical';
  if (percent >= 70) return 'warn';
  return 'ok';
}

/** 公钥缩写显示 */
export function shortKey(key: string, head = 20): string {
  if (!key) return '-';
  return key.length <= head ? key : `${key.slice(0, head)}...`;
}

/** 统一错误消息提取，供 catch 分支使用 */
export function messageOf(error: unknown, fallback: string): string {
  if (typeof error === 'object' && error !== null && 'message' in error) {
    const message = (error as { message?: unknown }).message;
    if (typeof message === 'string' && message.length > 0) return message;
  }
  return fallback;
}
