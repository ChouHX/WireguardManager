import { Box, Card, Group, Stack, Text, ThemeIcon, Title } from '@mantine/core';
import { IconCircleCheck, IconDeviceLaptop, IconShieldLock } from '@tabler/icons-react';
import { Outlet } from 'react-router-dom';

import { ColorSchemeToggle, LocaleToggle } from './Brand';
import { WireGuardLogo } from './WireGuardLogo';
import { useTranslation } from '@/i18n';
import { APP_BRAND } from '@/lib/navigation';

const HIGHLIGHTS = [
  { icon: IconShieldLock, key: 'adminWireguard' as const },
  { icon: IconDeviceLaptop, key: 'myWireguard' as const },
  { icon: IconCircleCheck, key: 'monitoring' as const },
];

/**
 * 认证页外壳：单卡片布局。
 * 卡片内左右横排——左侧品牌介绍（移动端隐藏），右侧表单，整体作为一个小容器居中。
 */
export function AuthLayout() {
  const { t } = useTranslation();

  return (
    <Box
      mih="100vh"
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '28px 16px',
        position: 'relative',
      }}
    >
      {/* 语言 / 主题切换浮在页面右上角，不占用卡片空间 */}
      <Group gap="xs" style={{ position: 'absolute', top: 16, right: 18 }}>
        <LocaleToggle />
        <ColorSchemeToggle />
      </Group>

      <Card
        className="wm-rise"
        w="100%"
        maw={920}
        p={0}
        radius="sm"
        style={{ overflow: 'hidden', boxShadow: '0 24px 64px -24px rgba(0, 0, 0, 0.42)' }}
      >
        <Box style={{ display: 'flex', minHeight: 508 }}>
          {/* 左侧品牌介绍：小屏隐藏，只保留表单 */}
          <Box
            className="wm-auth-hero"
            visibleFrom="sm"
            style={{
              flex: '0 0 348px',
              padding: 28,
              display: 'flex',
              flexDirection: 'column',
              justifyContent: 'space-between',
              position: 'relative',
            }}
          >
            <Stack gap="lg" style={{ position: 'relative', zIndex: 1 }}>
              <Group gap="sm" wrap="nowrap">
                <WireGuardLogo size={34} color="#fa6c61" />
                <Box>
                  <Text c="white" fw={650} fz={14} lh={1.2}>
                    {APP_BRAND.name}
                  </Text>
                  <Text
                    c="#8b93a1"
                    fz={10.5}
                    lh={1.3}
                    tt="uppercase"
                    style={{ letterSpacing: '0.1em' }}
                  >
                    {APP_BRAND.subtitle}
                  </Text>
                </Box>
              </Group>

              <Stack gap={6}>
                <Title order={2} c="white" fz={25} lh={1.2}>
                  {t('auth.heroTitle')}
                </Title>
                <Text c="#9aa2b1" fz={12.5} lh={1.55}>
                  {t('auth.heroDescription')}
                </Text>
              </Stack>

              <Stack gap={10}>
                {HIGHLIGHTS.map(({ icon: Icon, key }) => (
                  <Group key={key} gap={10} wrap="nowrap" align="flex-start">
                    <ThemeIcon variant="light" color="wg" size={28} radius="xs">
                      <Icon size={15} stroke={1.7} />
                    </ThemeIcon>
                    <Box>
                      <Text c="white" fz={12.5} fw={600} lh={1.3}>
                        {key === 'adminWireguard'
                          ? t('wireguard.adminTitle')
                          : key === 'myWireguard'
                            ? t('wireguard.managePeers')
                            : t('monitoring.monitorServer')}
                      </Text>
                      <Text c="#78808e" fz={11} lh={1.4}>
                        {key === 'adminWireguard'
                          ? t('wireguard.usersTraffic')
                          : key === 'myWireguard'
                            ? t('wireguard.autoGenerateNote')
                            : t('monitoring.systemResources')}
                      </Text>
                    </Box>
                  </Group>
                ))}
              </Stack>
            </Stack>

            <Text c="#5f6773" fz={10.5} style={{ position: 'relative', zIndex: 1 }}>
              namespace isolation · per-user wg interface · live traffic
            </Text>
          </Box>

          {/* 右侧表单容器 */}
          <Box
            style={{
              flex: '1 1 auto',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              padding: '32px 26px',
            }}
          >
            <Box w="100%" maw={352}>
              <Outlet />
            </Box>
          </Box>
        </Box>
      </Card>
    </Box>
  );
}
