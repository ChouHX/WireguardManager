import { Anchor, Box, Divider, Group, Text } from '@mantine/core';

import { useTranslation } from '@/i18n';
import { APP_BRAND } from '@/lib/navigation';

/** 页面底部信息条 */
export function AppFooter() {
  const { t } = useTranslation();

  return (
    <Box px="lg" py="sm">
      <Divider mb="sm" />
      <Group justify="space-between" gap="md" wrap="wrap">
        <Text size="xs" c="dimmed">
          {APP_BRAND.name} · {APP_BRAND.subtitle}
        </Text>
        <Group gap="sm">
          <Anchor
            href="https://www.wireguard.com/"
            target="_blank"
            rel="noreferrer"
            size="xs"
            c="dimmed"
          >
            WireGuard
          </Anchor>
          <Text size="xs" c="dimmed">
            {t('common.locale') === 'zh' ? '© 2024 保留所有权利' : '© 2024 All rights reserved'}
          </Text>
        </Group>
      </Group>
    </Box>
  );
}
