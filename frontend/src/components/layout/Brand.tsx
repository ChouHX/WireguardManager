import { ActionIcon, Menu, Tooltip, useMantineColorScheme } from '@mantine/core';
import { IconCheck, IconLanguage, IconMoon, IconSun } from '@tabler/icons-react';

import { useTranslation, type Locale } from '@/i18n';

/** 圆形按钮：圆角与尺寸解耦，避免受全局微圆角 token 影响 */
const CIRCLE_STYLES = { root: { borderRadius: '50%' } } as const;

const LOCALE_OPTIONS: { value: Locale; label: string }[] = [
  { value: 'zh', label: '中文' },
  { value: 'en', label: 'English' },
];

/** 明暗配色切换（正圆按钮） */
export function ColorSchemeToggle() {
  const { colorScheme, setColorScheme } = useMantineColorScheme();
  const isDark = colorScheme === 'dark';

  return (
    <Tooltip label={isDark ? 'Light' : 'Dark'} position="bottom" withArrow>
      <ActionIcon
        variant="default"
        size="md"
        styles={CIRCLE_STYLES}
        aria-label="Toggle color scheme"
        onClick={() => setColorScheme(isDark ? 'light' : 'dark')}
      >
        {isDark ? <IconSun size={16} stroke={1.7} /> : <IconMoon size={16} stroke={1.7} />}
      </ActionIcon>
    </Tooltip>
  );
}

/** 语言切换：正圆按钮 + 下拉菜单选择 */
export function LocaleToggle() {
  const { locale, setLocale } = useTranslation();

  return (
    <Menu shadow="md" width={148} position="bottom-end" withinPortal>
      <Menu.Target>
        <ActionIcon
          variant="default"
          size="md"
          styles={CIRCLE_STYLES}
          aria-label="Change language"
        >
          <IconLanguage size={16} stroke={1.7} />
        </ActionIcon>
      </Menu.Target>

      <Menu.Dropdown>
        {LOCALE_OPTIONS.map((option) => (
          <Menu.Item
            key={option.value}
            onClick={() => setLocale(option.value)}
            leftSection={
              <IconCheck
                size={14}
                stroke={2}
                style={{ opacity: locale === option.value ? 1 : 0 }}
              />
            }
            fw={locale === option.value ? 600 : 400}
          >
            {option.label}
          </Menu.Item>
        ))}
      </Menu.Dropdown>
    </Menu>
  );
}
