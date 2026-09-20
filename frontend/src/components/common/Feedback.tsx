import { Alert, Center, Stack, Text, ThemeIcon } from '@mantine/core';
import { IconAlertTriangle, IconInbox } from '@tabler/icons-react';
import type { ReactNode } from 'react';

/** 统一的错误提示条 */
export function ErrorAlert({ message, onClose }: { message?: string | null; onClose?: () => void }) {
  if (!message) return null;
  return (
    <Alert
      color="red"
      variant="light"
      icon={<IconAlertTriangle size={18} />}
      withCloseButton={Boolean(onClose)}
      onClose={onClose}
      radius="xs"
    >
      {message}
    </Alert>
  );
}

/** 空状态占位 */
export function EmptyState({
  title,
  description,
  action,
  icon,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
  icon?: ReactNode;
}) {
  return (
    <Center py={44}>
      <Stack align="center" gap="xs" maw={360}>
        <ThemeIcon variant="light" color="gray" size={44} radius="xs">
          {icon ?? <IconInbox size={22} />}
        </ThemeIcon>
        <Text fw={600}>{title}</Text>
        {description ? (
          <Text size="sm" c="dimmed" ta="center">
            {description}
          </Text>
        ) : null}
        {action}
      </Stack>
    </Center>
  );
}
