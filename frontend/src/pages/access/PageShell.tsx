import { useEffect } from 'react';
import type { ReactNode } from 'react';
import { ConfigProvider, Layout, message } from 'antd';

import { useTheme } from '@/hooks/useTheme';
import AppSidebar from '@/layouts/AppSidebar';
import { setMessageInstance } from '@/utils/messageBus';

// The frame every access page shares: theme, sidebar, content area and the message bus.
export default function PageShell({
  className,
  children,
}: {
  className: string;
  children: ReactNode;
}) {
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);
  const pageClass = ['panel-page', className, isDark ? 'is-dark' : '', isUltra ? 'is-ultra' : '']
    .filter(Boolean)
    .join(' ');
  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            {children}
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
