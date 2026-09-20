import { Anchor, Breadcrumbs, Group, Stack, Text, Title } from '@mantine/core';
import { Link } from 'react-router-dom';
import type { ReactNode } from 'react';

export interface Crumb {
  label: string;
  to?: string;
}

interface PageHeaderProps {
  title: string;
  subtitle?: string;
  crumbs?: Crumb[];
  actions?: ReactNode;
}

/** 页面标题区：面包屑 + 标题 + 右侧操作，统一各页面的观感 */
export function PageHeader({ title, subtitle, crumbs, actions }: PageHeaderProps) {
  return (
    <Stack gap="xs" className="wm-rise" style={{ '--wm-delay': '0ms' } as React.CSSProperties}>
      {crumbs && crumbs.length > 0 ? (
        <Breadcrumbs separator="/" separatorMargin="xs">
          {crumbs.map((crumb, index) =>
            crumb.to ? (
              <Anchor key={`${crumb.to}-${index}`} component={Link} to={crumb.to} size="xs" c="dimmed">
                {crumb.label}
              </Anchor>
            ) : (
              <Text key={`${crumb.label}-${index}`} size="xs" c="dimmed">
                {crumb.label}
              </Text>
            ),
          )}
        </Breadcrumbs>
      ) : null}

      <Group justify="space-between" align="flex-end" gap="sm" wrap="wrap">
        <div>
          <Title order={3}>{title}</Title>
          {subtitle ? (
            <Text fz={12.5} c="dimmed" mt={2}>
              {subtitle}
            </Text>
          ) : null}
        </div>
        {actions ? <Group gap="xs">{actions}</Group> : null}
      </Group>
    </Stack>
  );
}
