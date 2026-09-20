import { ActionIcon, AppShell, Box, Burger, Group, Text, Tooltip } from '@mantine/core';
import { useDisclosure, useLocalStorage } from '@mantine/hooks';
import { IconChevronLeft, IconChevronRight } from '@tabler/icons-react';
import { useMemo } from 'react';
import { Link, Outlet, useLocation } from 'react-router-dom';

import { AppFooter } from './AppFooter';
import { ColorSchemeToggle, LocaleToggle } from './Brand';
import { SideNav } from './SideNav';
import { UserMenu } from './UserMenu';
import { WireGuardLogo } from './WireGuardLogo';
import { ErrorBoundary } from '@/components/common/ErrorBoundary';
import { useTranslation } from '@/i18n';
import { APP_BRAND, ROUTE_TITLES } from '@/lib/navigation';

const NAV_COLLAPSED_KEY = 'wm-nav-collapsed';

/**
 * 控制台主框架（紧凑式）：
 * 采用 Mantine 默认布局 —— 顶栏横跨整宽，官方 WireGuard logo 与顶栏同一行，
 * 侧边导航位于顶栏下方（layout="alt" 是"侧栏贯穿全高"的另一种排布，此处不适用）。
 */
export function AppLayout() {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  const [mobileOpened, { toggle: toggleMobile, close: closeMobile }] = useDisclosure(false);
  const [collapsed, setCollapsed] = useLocalStorage({
    key: NAV_COLLAPSED_KEY,
    defaultValue: false,
    getInitialValueInEffect: true,
  });

  const navWidth = collapsed ? 64 : 226;
  const pageTitle = useMemo(() => {
    const key = Object.keys(ROUTE_TITLES).find(
      (route) => pathname === route || pathname.startsWith(`${route}/`),
    );
    return key ? t(ROUTE_TITLES[key]) : APP_BRAND.name;
  }, [pathname, t]);

  return (
    <AppShell
      header={{ height: 52 }}
      navbar={{ width: navWidth, breakpoint: 'md', collapsed: { mobile: !mobileOpened } }}
      padding={0}
      withBorder={false}
      styles={{
        main: {
          background: 'transparent',
          minHeight: '100vh',
          display: 'flex',
          flexDirection: 'column',
        },
      }}
    >
      <AppShell.Header className="wm-header">
        <Group h="100%" px="sm" justify="space-between" wrap="nowrap">
          <Group gap={6} wrap="nowrap">
            <Burger opened={mobileOpened} onClick={toggleMobile} hiddenFrom="md" size="sm" />

            <Tooltip
              label={collapsed ? t('common.expand') : t('common.collapse')}
              position="bottom"
              withArrow
            >
              <ActionIcon
                variant="subtle"
                color="gray"
                size="md"
                visibleFrom="md"
                onClick={() => setCollapsed((prev) => !prev)}
                aria-label={collapsed ? t('common.expand') : t('common.collapse')}
              >
                {collapsed ? <IconChevronRight size={16} /> : <IconChevronLeft size={16} />}
              </ActionIcon>
            </Tooltip>

            <Box className="wm-header-divider" visibleFrom="sm" />

            {/* 官方 WireGuard logo + 产品名，与顶栏同一行 */}
            <Box component={Link} to="/dashboard" className="wm-brand">
              <WireGuardLogo size={24} />
              <Box visibleFrom="xs">
                <Text fw={650} fz={13.5} lh={1.15}>
                  {APP_BRAND.name}
                </Text>
                <Text fz={10} c="dimmed" lh={1.2} tt="uppercase" style={{ letterSpacing: '0.08em' }}>
                  {APP_BRAND.subtitle}
                </Text>
              </Box>
            </Box>

            <Box className="wm-header-divider" visibleFrom="md" />

            <Text fz={13} c="dimmed" truncate visibleFrom="md">
              {pageTitle}
            </Text>
          </Group>

          <Group gap={6} wrap="nowrap">
            <LocaleToggle />
            <ColorSchemeToggle />
            <UserMenu />
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar className="wm-nav" p={0}>
        <SideNav collapsed={collapsed} onNavigate={closeMobile} />
      </AppShell.Navbar>

      <AppShell.Main>
        <Box flex={1} px={{ base: 'md', sm: 'lg' }} py="md">
          {/* 单页渲染错误不会拖垮整体框架，切换路由会自动复位 */}
          <ErrorBoundary resetKey={pathname}>
            <Outlet />
          </ErrorBoundary>
        </Box>
        <AppFooter />
      </AppShell.Main>
    </AppShell>
  );
}
