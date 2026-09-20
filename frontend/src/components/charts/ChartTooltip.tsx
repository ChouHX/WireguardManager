import { Box, Stack, Text } from '@mantine/core';

export interface TooltipEntry {
  name?: string | number;
  value?: number | string;
  color?: string;
  dataKey?: string | number;
}

export interface ChartTooltipProps {
  active?: boolean;
  label?: string | number;
  payload?: TooltipEntry[];
  /** 数值格式化函数 */
  formatter: (value: number, dataKey: string) => string;
}

/** 图表统一 tooltip：与 Mantine 卡片视觉一致，避免使用默认白底样式 */
export function ChartTooltip({ active, label, payload, formatter }: ChartTooltipProps) {
  if (!active || !payload || payload.length === 0) return null;

  return (
    <Box
      p="xs"
      style={{
        borderRadius: 3,
        border: '1px solid var(--mantine-color-default-border)',
        background: 'var(--mantine-color-body)',
        boxShadow: '0 8px 24px rgba(0, 0, 0, 0.18)',
        minWidth: 140,
      }}
    >
      <Text size="xs" c="dimmed" mb={6}>
        {label}
      </Text>
      <Stack gap={4}>
        {payload.map((entry, index) => (
          <Box key={`${entry.dataKey}-${index}`} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Box
              style={{
                width: 8,
                height: 8,
                borderRadius: 2,
                background: entry.color ?? 'var(--mantine-color-wg-6)',
                flex: '0 0 auto',
              }}
            />
            <Text size="xs" c="dimmed" style={{ flex: 1 }}>
              {entry.name}
            </Text>
            <Text size="xs" fw={650} className="wm-mono">
              {formatter(Number(entry.value ?? 0), String(entry.dataKey ?? ''))}
            </Text>
          </Box>
        ))}
      </Stack>
    </Box>
  );
}
