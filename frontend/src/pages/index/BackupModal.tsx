import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Checkbox, Modal } from 'antd';
import { DownloadOutlined, UploadOutlined } from '@ant-design/icons';

import { HttpUtil, PromiseUtil } from '@/utils';
import type { MigrationReport } from '@/generated/types';
import MigrationModal from './MigrationModal';
import './BackupModal.css';

interface BusyEvent {
  busy: boolean;
  tip?: string;
}

interface BackupModalProps {
  open: boolean;
  basePath: string;
  onClose: () => void;
  onBusy: (e: BusyEvent) => void;
}

export default function BackupModal({
  open,
  basePath: _basePath,
  onClose,
  onBusy,
}: BackupModalProps) {
  const { t } = useTranslation();
  const isPostgres = window.X_UI_DB_TYPE === 'postgres';
  const [keepHostSettings, setKeepHostSettings] = useState(true);
  const [inspected, setInspected] = useState<{ file: File; report: MigrationReport } | null>(null);
  const [inspecting, setInspecting] = useState(false);

  function exportFullBackup() {
    window.location.href = (window.X_UI_BASE_PATH || '') + 'panel/api/server/getFullBackup';
  }

  function inspectFullBackup() {
    const fileInput = document.createElement('input');
    fileInput.type = 'file';
    fileInput.accept = '.tar.gz,.tgz,application/gzip';
    fileInput.addEventListener('change', async (e) => {
      const file = (e.target as HTMLInputElement).files?.[0];
      if (!file) return;
      const form = new FormData();
      form.append('backup', file);
      setInspecting(true);
      try {
        const msg = await HttpUtil.post<MigrationReport>('/panel/api/server/inspectBackup', form, {
          headers: { 'Content-Type': 'multipart/form-data' },
        });
        if (msg?.success && msg.obj) setInspected({ file, report: msg.obj });
      } finally {
        setInspecting(false);
      }
    });
    fileInput.click();
  }

  function exportDb() {
    window.location.href = (window.X_UI_BASE_PATH || '') + 'panel/api/server/getDb';
  }

  function exportMigration() {
    window.location.href = (window.X_UI_BASE_PATH || '') + 'panel/api/server/getMigration';
  }

  function importDb() {
    const fileInput = document.createElement('input');
    fileInput.type = 'file';
    fileInput.accept = '.dump,.db';
    fileInput.addEventListener('change', async (e) => {
      const dbFile = (e.target as HTMLInputElement).files?.[0];
      if (!dbFile) return;

      const formData = new FormData();
      formData.append('db', dbFile);
      formData.append('keepHostSettings', String(keepHostSettings));

      onClose();
      onBusy({ busy: true, tip: `${t('pages.index.importDatabase')}…` });

      const upload = await HttpUtil.post('/panel/api/server/importDB', formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
      });
      if (!upload?.success) {
        onBusy({ busy: false });
        return;
      }

      // importDB schedules the panel restart server-side; wait it out, then reload.
      onBusy({ busy: true, tip: `${t('pages.settings.restartPanel')}…` });
      await PromiseUtil.sleep(5000);
      window.location.reload();
    });
    fileInput.click();
  }

  return (
    <Modal open={open} title={t('pages.index.backupTitle')} footer={null} onCancel={onClose}>
      {isPostgres && (
        <div className="backup-description" style={{ marginBottom: 16 }}>
          {t('pages.index.backupPostgresNote')}
        </div>
      )}
      <div className="backup-list" style={{ marginBottom: 16 }}>
        <div className="backup-item">
          <div className="backup-meta">
            <div className="backup-title">{t('pages.index.migration.download')}</div>
            <div className="backup-description">{t('pages.index.migration.downloadDesc')}</div>
          </div>
          <Button
            type="primary"
            aria-label={t('pages.index.migration.download')}
            onClick={exportFullBackup}
            icon={<DownloadOutlined aria-hidden />}
          />
        </div>
        <div className="backup-item">
          <div className="backup-meta">
            <div className="backup-title">{t('pages.index.migration.upload')}</div>
            <div className="backup-description">{t('pages.index.migration.uploadDesc')}</div>
          </div>
          <Button
            type="primary"
            loading={inspecting}
            aria-label={t('pages.index.migration.upload')}
            onClick={inspectFullBackup}
            icon={<UploadOutlined aria-hidden />}
          />
        </div>
      </div>
      <div className="backup-list">
        <div className="backup-item">
          <div className="backup-meta">
            <div className="backup-title">{t('pages.index.exportDatabase')}</div>
            <div className="backup-description">
              {isPostgres
                ? t('pages.index.exportDatabasePgDesc')
                : t('pages.index.exportDatabaseDesc')}
            </div>
          </div>
          <Button
            type="primary"
            aria-label={t('pages.index.exportDatabase')}
            onClick={exportDb}
            icon={<DownloadOutlined aria-hidden />}
          />
        </div>

        {isPostgres && (
          <div className="backup-item">
            <div className="backup-meta">
              <div className="backup-title">{t('pages.index.migrationDownload')}</div>
              <div className="backup-description">{t('pages.index.migrationDownloadPgDesc')}</div>
            </div>
            <Button
              type="primary"
              aria-label={t('pages.index.migrationDownload')}
              onClick={exportMigration}
              icon={<DownloadOutlined aria-hidden />}
            />
          </div>
        )}

        <div className="backup-item">
          <div className="backup-meta">
            <div className="backup-title">{t('pages.index.importDatabase')}</div>
            <div className="backup-description">
              {isPostgres
                ? t('pages.index.importDatabasePgDesc')
                : t('pages.index.importDatabaseDesc')}
            </div>
          </div>
          <Button
            type="primary"
            aria-label={t('pages.index.importDatabase')}
            onClick={importDb}
            icon={<UploadOutlined aria-hidden />}
          />
        </div>

        <div className="backup-item">
          <div className="backup-meta">
            <Checkbox
              checked={keepHostSettings}
              onChange={(e) => setKeepHostSettings(e.target.checked)}
            >
              {t('pages.index.importKeepHostSettings')}
            </Checkbox>
            <div className="backup-description">{t('pages.index.importKeepHostSettingsDesc')}</div>
          </div>
        </div>
      </div>
      {inspected && (
        <MigrationModal
          file={inspected.file}
          report={inspected.report}
          onBusy={onBusy}
          onClose={() => setInspected(null)}
        />
      )}
    </Modal>
  );
}
