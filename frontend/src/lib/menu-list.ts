import {
  Users,
  Settings,
  LayoutGrid,
  Network,
  Shield,
  Activity,
  LucideIcon
} from "lucide-react";

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
};

type Group = {
  groupLabel: string;
  menus: Menu[];
};

export function getMenuList(pathname: string, t?: (key: string) => string): Group[] {
  const translate = t || ((key: string) => key);
  
  return [
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
          icon: Shield
        }
      ]
    },
    {
      groupLabel: translate("nav.settings"),
      menus: [
        {
          href: "/users",
          label: translate("nav.users"),
          icon: Users
        },
        {
          href: "/account",
          label: translate("nav.account"),
          icon: Settings
        }
      ]
    }
  ];
}
