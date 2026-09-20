import { Box, Divider, Group, NavLink, ScrollArea, Stack, Text, Tooltip } from '@mantine/core';
import { IconCircleFilled } from '@tabler/icons-react';
import { useMemo } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';

import { useTranslation } from '@/i18n';
import { getNavigation } from '@/lib/navigation';
import { useAuthStore } from '@/stores/auth-store';

interface SideNavProps {
  collapsed: boolean;
  /** 移动端点击导航后收起抽屉 */
  onNavigate?: () => void;
}

/**
 * 侧边导航：紧凑排布，品牌区已上移至顶栏，这里只承载导航与版本信息。
 */
export function SideNav({ collapsed, onNavigate }: SideNavProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const role = useAuthStore((state) => state.user?.role);

  const groups = useMemo(() => getNavigation(role), [role]);

  return (
    <Stack h="100%" gap={0}>
      <ScrollArea flex={1} px={collapsed ? 6 : 8} py="sm" scrollbarSize={5}>
        <Stack gap="md">
          {groups.map((group) => (
            <Box key={group.key}>
              {group.labelKey && !collapsed ? (
                <Text className="wm-nav-section" px={8} mb={4}>
                  {t(group.labelKey)}
                </Text>
              ) : null}

              <Stack gap={2}>
                {group.items.map((item) => {
                  const active = pathname === item.to || pathname.startsWith(`${item.to}/`);
                  const label = t(item.labelKey);

                  const node = (
                    <NavLink
                      key={item.to}
                      className="wm-nav-link"
                      active={active}
                      label={collapsed ? undefined : label}
                      leftSection={<item.icon size={17} stroke={1.6} />}
                      onClick={() => {
                        navigate(item.to);
                        onNavigate?.();
                      }}
                      variant="subtle"
                      styles={{
                        root: { borderRadius: 3, paddingBlock: 6, paddingInline: 8 },
                        label: { fontSize: 13, fontWeight: 550 },
                      }}
                    />
                  );

                  return collapsed ? (
                    <Tooltip key={item.to} label={label} position="right" withArrow openDelay={150}>
                      <Box>{node}</Box>
                    </Tooltip>
                  ) : (
                    node
                  );
                })}
              </Stack>
            </Box>
          ))}
        </Stack>
      </ScrollArea>

      <Divider color="#262b34" />

      <Group justify={collapsed ? 'center' : 'flex-start'} gap={6} px="sm" py={7}>
        <IconCircleFilled size={6} color="#26c7ba" />
        {!collapsed ? (
          <Text fz={11} c="#6b7381">
            v1.0.0
          </Text>
        ) : null}
      </Group>
    </Stack>
  );
}
