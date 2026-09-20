import { Component, type ErrorInfo, type ReactNode } from 'react';
import { Button, Card, Code, Group, Stack, Text, ThemeIcon, Title } from '@mantine/core';
import { IconBug, IconRefresh, IconArrowLeft } from '@tabler/icons-react';
import { Link } from 'react-router-dom';

import { useTranslation } from '@/i18n';

interface ErrorBoundaryProps {
  children: ReactNode;
  /**
   * 变化时自动清除已捕获的错误。路由切换时传入新路径，
   * 避免一次失败把后续页面一起锁死在错误态。
   */
  resetKey?: string;
}

interface ErrorBoundaryState {
  error: Error | null;
}

/** 错误兜底界面（函数组件，便于使用 i18n hook） */
function ErrorFallback({ error, onRetry }: { error: Error; onRetry: () => void }) {
  const { t } = useTranslation();

  return (
    <Card withBorder radius="xs" className="wm-rise">
      <Stack gap="md" align="flex-start">
        <Group gap="sm">
          <ThemeIcon variant="light" color="red" size={32} radius="xs">
            <IconBug size={18} stroke={1.7} />
          </ThemeIcon>
          <Stack gap={0}>
            <Title order={4}>{t('errors.boundaryTitle')}</Title>
            <Text fz={12.5} c="dimmed">
              {t('errors.boundaryDescription')}
            </Text>
          </Stack>
        </Group>

        <Code block className="wm-code-block" w="100%">
          {error.message || String(error)}
        </Code>

        <Group gap="xs">
          <Button color="wg" radius="xs" leftSection={<IconRefresh size={15} />} onClick={onRetry}>
            {t('errors.boundaryRetry')}
          </Button>
          <Button
            component={Link}
            to="/dashboard"
            variant="default"
            radius="xs"
            leftSection={<IconArrowLeft size={15} />}
          >
            {t('errors.boundaryBack')}
          </Button>
        </Group>
      </Stack>
    </Card>
  );
}

/**
 * 渲染期错误边界：单个页面崩溃时保留整体框架，
 * 并把错误信息与恢复入口直接展示给用户，而不是白屏。
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[ErrorBoundary] render error:', error, info.componentStack);
  }

  componentDidUpdate(prevProps: ErrorBoundaryProps) {
    if (this.state.error && prevProps.resetKey !== this.props.resetKey) {
      this.setState({ error: null });
    }
  }

  render() {
    if (this.state.error) {
      return <ErrorFallback error={this.state.error} onRetry={() => this.setState({ error: null })} />;
    }
    return this.props.children;
  }
}
