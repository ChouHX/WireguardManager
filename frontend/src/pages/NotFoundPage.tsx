import type { CSSProperties } from 'react';
import { Button, Card, Center, Stack, Text, Title } from '@mantine/core';
import { IconHome } from '@tabler/icons-react';
import { Link } from 'react-router-dom';

import { useTranslation } from '@/i18n';

/** 独立整页 404：不依赖 AppLayout，背景网格沿用全局 body 样式 */
export default function NotFoundPage() {
  const { t } = useTranslation();

  return (
    <Center mih="100vh" px="md" py="xl">
      <Card
        className="wm-rise"
        maw={460}
        w="100%"
        style={{ '--wm-delay': '60ms' } as CSSProperties}
      >
        <Stack align="center" gap="sm">
          <Text className="wm-mono" fw={700} fz={68} lh={1} c="wg.6">
            404
          </Text>

          <Title order={3} ta="center">
            {t('errors.notFoundTitle')}
          </Title>

          <Text size="sm" c="dimmed" ta="center">
            {t('errors.notFoundDescription')}
          </Text>

          <Button
            component={Link}
            to="/dashboard"
            color="wg"
            mt="sm"
            leftSection={<IconHome size={16} />}
          >
            {t('common.back')}
          </Button>
        </Stack>
      </Card>
    </Center>
  );
}
