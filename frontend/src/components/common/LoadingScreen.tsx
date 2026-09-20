import { Center, Loader, Stack, Text } from '@mantine/core';

/** 全屏加载态：会话恢复、路由切换过渡使用 */
export function LoadingScreen({ label }: { label?: string }) {
  return (
    <Center mih="70vh">
      <Stack align="center" gap="sm">
        <Loader color="wg" size="md" type="dots" />
        {label ? (
          <Text size="sm" c="dimmed">
            {label}
          </Text>
        ) : null}
      </Stack>
    </Center>
  );
}

/** 区块级加载态：卡片内部使用 */
export function InlineLoader({ label }: { label?: string }) {
  return (
    <Center py="xl">
      <Stack align="center" gap="xs">
        <Loader color="wg" size="sm" />
        {label ? (
          <Text size="xs" c="dimmed">
            {label}
          </Text>
        ) : null}
      </Stack>
    </Center>
  );
}
