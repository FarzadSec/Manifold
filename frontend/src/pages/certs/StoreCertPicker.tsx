import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Select } from 'antd';

import { keys } from '@/api/queryKeys';
import type { CertificateView } from '@/generated/types';
import { HttpUtil } from '@/utils';

interface StoreCertPickerProps {
  onPick: (certFile: string, keyFile: string) => void;
}

// StoreCertPicker fills an inbound's certificate paths from the certificate store.
export default function StoreCertPicker({ onPick }: StoreCertPickerProps) {
  const { t } = useTranslation();
  const list = useQuery({
    queryKey: keys.certs.list(),
    queryFn: async () => {
      const msg = await HttpUtil.get<CertificateView[]>('/panel/api/certs/list', undefined, {
        silent: true,
      });
      return msg?.success && Array.isArray(msg.obj) ? msg.obj : [];
    },
  });
  const ready = (list.data ?? []).filter((c) => c.notAfter > 0);
  if (ready.length === 0) return null;
  return (
    <Select<number>
      style={{ minWidth: 200 }}
      placeholder={t('pages.certs.fromStore')}
      aria-label={t('pages.certs.fromStore')}
      value={null}
      onChange={(id) => {
        const c = ready.find((x) => x.id === id);
        if (c) onPick(c.certFile, c.keyFile);
      }}
      options={ready.map((c) => ({ value: c.id, label: `${c.name} · ${c.domains.join(', ')}` }))}
    />
  );
}
