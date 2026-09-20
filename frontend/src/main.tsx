import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { MantineProvider } from '@mantine/core';
import { Notifications } from '@mantine/notifications';

import '@mantine/core/styles.css';
import '@mantine/notifications/styles.css';
import './styles/global.css';

import { App } from './App';
import { theme } from './theme';
import { I18nProvider } from './i18n';

const container = document.getElementById('root');
if (!container) {
  throw new Error('Root container #root not found');
}

createRoot(container).render(
  <StrictMode>
    <MantineProvider theme={theme} defaultColorScheme="auto">
      <I18nProvider>
        <Notifications position="top-right" limit={4} autoClose={3200} />
        <App />
      </I18nProvider>
    </MantineProvider>
  </StrictMode>,
);
