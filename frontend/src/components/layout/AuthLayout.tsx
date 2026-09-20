import { Box, Group, Stack, Text, ThemeIcon, Title } from '@mantine/core';
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

/** 认证页外壳：左侧品牌叙事面板 + 右侧表单 */
export function AuthLayout() {
  const { t } = useTranslation();

  return (
    <Box style={{ display: 'flex', minHeight: '100vh' }}>
      {/* 左侧品牌面板（小屏隐藏） */}
      <Box
        className="wm-auth-hero"
        visibleFrom="md"
        style={{ flex: '1 1 52%', display: 'flex', alignItems: 'center', padding: 36 }}
      >
        <Stack gap="md" maw={520} style={{ position: 'relative', zIndex: 1 }}>
          <Group gap="sm">
            {/* 官方 WireGuard 标志 */}
            <WireGuardLogo size={40} color="#f08a80" />
            <Box>
              <Text c="white" fw={650} fz={15} lh={1.2}>
                {APP_BRAND.name}
              </Text>
              <Text c="#8b93a1" fz={11.5} lh={1.2} tt="uppercase" style={{ letterSpacing: '0.1em' }}>
                {APP_BRAND.subtitle}
              </Text>
            </Box>
          </Group>

          <Stack gap="xs">
            <Title order={1} c="white" fz={34} lh={1.15}>
              {t('auth.loginTitle')}
            </Title>
            <Text c="#9aa2b1" fz={14} maw={440}>
              {t('auth.loginDescription')}
            </Text>
          </Stack>

          <Stack gap="sm">
            {HIGHLIGHTS.map(({ icon: Icon, key }) => (
              <Group key={key} gap="sm" wrap="nowrap">
                <ThemeIcon variant="light" color="wg" size={34} radius="xs">
                  <Icon size={18} stroke={1.7} />
                </ThemeIcon>
                <Box>
                  <Text c="white" fz={14} fw={600}>
                    {key === 'adminWireguard'
                      ? t('wireguard.adminTitle')
                      : key === 'myWireguard'
                        ? t('wireguard.managePeers')
                        : t('monitoring.monitorServer')}
                  </Text>
                  <Text c="#78808e" fz={12}>
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

          <Text c="#5f6773" fz={12}>
            namespace isolation · per-user wg interface · live traffic
          </Text>
        </Stack>
      </Box>

      {/* 右侧表单区 */}
      <Box
        style={{
          flex: '1 1 48%',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          padding: '32px 20px',
          position: 'relative',
        }}
      >
        <Group gap="xs" style={{ position: 'absolute', top: 18, right: 20 }}>
          <LocaleToggle />
          <ColorSchemeToggle />
        </Group>

        <Box w="100%" maw={396} className="wm-rise">
          <Outlet />
        </Box>
      </Box>
    </Box>
  );
}
