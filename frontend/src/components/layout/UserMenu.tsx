import { Avatar, Group, Menu, Text, UnstyledButton } from '@mantine/core';
import { IconLogout, IconSettings, IconUser } from '@tabler/icons-react';
import { useNavigate } from 'react-router-dom';

import { useTranslation } from '@/i18n';
import { useAuthStore } from '@/stores/auth-store';

/**
 * 顶栏用户区：仅显示用户名 + 头像（无边框、无底色卡片），点击任一元素弹出菜单。
 */
export function UserMenu() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const user = useAuthStore((state) => state.user);
  const logout = useAuthStore((state) => state.logout);

  if (!user) return null;

  const initial = (user.name || user.email).trim().charAt(0).toUpperCase();

  return (
    <Menu shadow="md" width={212} position="bottom-end" withinPortal>
      <Menu.Target>
        <UnstyledButton className="wm-user-trigger" aria-label={t('nav.account')}>
          <Group gap={8} wrap="nowrap">
            <Text fz={13} fw={550} lh={1} truncate maw={150} visibleFrom="sm">
              {user.name || user.email}
            </Text>
            <Avatar color="wg" radius="50%" size={26}>
              {initial}
            </Avatar>
          </Group>
        </UnstyledButton>
      </Menu.Target>

      <Menu.Dropdown>
        <Menu.Label>
          <Text fz={11} c="dimmed" truncate maw={180}>
            {user.email}
          </Text>
        </Menu.Label>
        <Menu.Divider />
        <Menu.Item
          leftSection={<IconUser size={15} stroke={1.7} />}
          onClick={() => navigate('/account')}
        >
          {t('nav.account')}
        </Menu.Item>
        <Menu.Item
          leftSection={<IconSettings size={15} stroke={1.7} />}
          onClick={() => navigate('/account')}
        >
          {t('user.updateProfile')}
        </Menu.Item>
        <Menu.Divider />
        <Menu.Item
          color="red"
          leftSection={<IconLogout size={15} stroke={1.7} />}
          onClick={() => {
            logout();
            navigate('/auth/login', { replace: true });
          }}
        >
          {t('nav.logout')}
        </Menu.Item>
      </Menu.Dropdown>
    </Menu>
  );
}
