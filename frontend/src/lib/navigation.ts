import {
  IconDeviceLaptop,
  IconLayoutDashboard,
  IconSettings,
  IconShieldLock,
  IconUsers,
  type Icon,
} from '@tabler/icons-react';
import type { UserRole } from '@/types/auth';

export interface NavItem {
  /** 路由地址 */
  to: string;
  /** i18n key */
  labelKey: string;
  icon: Icon;
  /** 仅这些角色可见；缺省表示所有登录用户可见 */
  roles?: UserRole[];
  /** 导航组归属 */
  group: 'main' | 'wireguard' | 'settings';
}

export interface NavGroup {
  key: 'main' | 'wireguard' | 'settings';
  labelKey: string | null;
  items: NavItem[];
}

const NAV_ITEMS: NavItem[] = [
  { to: '/dashboard', labelKey: 'nav.dashboard', icon: IconLayoutDashboard, group: 'main' },
  { to: '/wireguard', labelKey: 'nav.myWireguard', icon: IconDeviceLaptop, group: 'wireguard' },
  {
    to: '/admin-wireguard',
    labelKey: 'nav.adminWireguard',
    icon: IconShieldLock,
    roles: ['admin'],
    group: 'wireguard',
  },
  { to: '/users', labelKey: 'nav.users', icon: IconUsers, roles: ['admin'], group: 'settings' },
  { to: '/account', labelKey: 'nav.account', icon: IconSettings, group: 'settings' },
];

const GROUP_LABELS: Record<NavGroup['key'], string | null> = {
  main: null,
  wireguard: 'nav.wireguard',
  settings: 'nav.settings',
};

/** 按角色过滤后的导航分组 */
export function getNavigation(role: UserRole | undefined): NavGroup[] {
  const visible = NAV_ITEMS.filter((item) => !item.roles || (role && item.roles.includes(role)));
  const order: NavGroup['key'][] = ['main', 'wireguard', 'settings'];

  return order
    .map((key) => ({
      key,
      labelKey: GROUP_LABELS[key],
      items: visible.filter((item) => item.group === key),
    }))
    .filter((group) => group.items.length > 0);
}

/** 页面标题（AppShell header 使用） */
export const ROUTE_TITLES: Record<string, string> = {
  '/dashboard': 'monitoring.title',
  '/wireguard': 'wireguard.title',
  '/admin-wireguard': 'wireguard.adminTitle',
  '/users': 'users.title',
  '/account': 'user.profile',
};

export const APP_BRAND = {
  name: 'WireGuard Manager',
  short: 'WM',
  subtitle: 'Control Plane',
} as const;
