import {
  Users,
  Settings,
  LayoutGrid,
  Network,
  Shield,
  Activity,
  LucideIcon
} from "lucide-react";
import { UserRole } from "@/types/auth";

type Submenu = {
  href: string;
  label: string;
  active?: boolean;
};

type Menu = {
  href: string;
  label: string;
  active?: boolean;
  icon: LucideIcon;
  submenus?: Submenu[];
  roles?: UserRole[];
};

type Group = {
  groupLabel: string;
  menus: Menu[];
};

export function getMenuList(
  pathname: string,
  t?: (key: string) => string,
  role?: UserRole
): Group[] {
  const translate = t || ((key: string) => key);
  const currentRole = role ?? UserRole.NORMAL_USER;

  const groups: Group[] = [
    {
      groupLabel: "",
      menus: [
        {
          href: "/dashboard",
          label: translate("nav.dashboard"),
          icon: LayoutGrid,
          submenus: []
        }
      ]
    },
    {
      groupLabel: translate("nav.wireguard"),
      menus: [
        {
          href: "/wireguard",
          label: translate("nav.myWireguard"),
          icon: Network
        },
        {
          href: "/admin-wireguard",
          label: translate("nav.adminWireguard"),
          icon: Shield,
          roles: [UserRole.ADMIN]
        }
      ]
    },
    {
      groupLabel: translate("nav.settings"),
      menus: [
        {
          href: "/users",
          label: translate("nav.users"),
          icon: Users,
          roles: [UserRole.ADMIN]
        },
        {
          href: "/account",
          label: translate("nav.account"),
          icon: Settings
        }
      ]
    }
  ];

  return groups
    .map((group) => ({
      ...group,
      menus: group.menus.filter(
        (menu) => !menu.roles || menu.roles.includes(currentRole)
      )
    }))
    .filter((group) => group.menus.length > 0);
}
