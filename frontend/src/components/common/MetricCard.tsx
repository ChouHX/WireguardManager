import { Box, Card, Group, Progress, Stack, Text, ThemeIcon } from '@mantine/core';
import type { Icon } from '@tabler/icons-react';
import type { ReactNode } from 'react';

import type { LoadLevel } from '@/lib/format';

type Accent = 'wg' | 'teal' | 'blue' | 'green' | 'yellow' | 'orange' | 'gray';

interface MetricCardProps {
  label: string;
  value: ReactNode;
  hint?: ReactNode;
  icon: Icon;
  accent?: Accent;
  /** 0-100 的进度展示，level 决定进度条配色 */
  progress?: { value: number; level?: LoadLevel };
  delay?: number;
}

const ACCENT_COLORS: Record<Accent, string> = {
  wg: 'wg',
  teal: 'teal',
  blue: 'blue',
  green: 'green',
  yellow: 'yellow',
  orange: 'orange',
  gray: 'gray',
};

const LEVEL_COLORS: Record<LoadLevel, string> = {
  ok: 'teal',
  warn: 'yellow',
  critical: 'red',
};

/** 指标卡：图标徽标 + 主数值 + 进度条，用于资源/流量概览 */
export function MetricCard({
  label,
  value,
  hint,
  icon: Icon,
  accent = 'wg',
  progress,
  delay = 0,
}: MetricCardProps) {
  const color = ACCENT_COLORS[accent];
  const progressColor = progress?.level ? LEVEL_COLORS[progress.level] : color;

  return (
    <Card
      className="wm-rise"
      style={{ '--wm-delay': `${delay}ms` } as React.CSSProperties}
      padding="md"
    >
      <Stack gap={6}>
        <Group justify="space-between" wrap="nowrap" align="flex-start">
          <Text fz={11} fw={600} c="dimmed" tt="uppercase" style={{ letterSpacing: '0.08em' }}>
            {label}
          </Text>
          <ThemeIcon variant="light" color={color} size={26} radius="xs">
            <Icon size={15} stroke={1.7} />
          </ThemeIcon>
        </Group>

        {/* 用 Box 而非 Text：value 可能是 ReactNode（如多行速率），
            避免在 <p> 内嵌 <div> 触发 HTML 嵌套告警 */}
        <Box className="wm-mono" fz={22} fw={700} lh={1.1}>
          {value}
        </Box>

        {progress ? (
          <Progress
            value={Math.min(100, Math.max(0, progress.value))}
            color={progressColor}
            size={6}
            radius="xs"
          />
        ) : null}

        {hint ? (
          <Text size="xs" c="dimmed">
            {hint}
          </Text>
        ) : null}
      </Stack>
    </Card>
  );
}
