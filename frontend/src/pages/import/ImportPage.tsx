import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  Col,
  Descriptions,
  Form,
  Input,
  Result,
  Row,
  Segmented,
  Select,
  Space,
  Steps,
  Switch,
  Table,
  Tag,
  Typography,
  Upload,
} from 'antd';
import { InboxOutlined } from '@ant-design/icons';

import { selectTagRender } from '@/components/form';
import { useInboundOptions } from '@/api/queries/useInboundOptions';
import type { ImportApplyResult, ImportPreview, ImportUserPreview } from '@/generated/types';
import { useDatepicker } from '@/hooks/useDatepicker';
import PageShell from '@/pages/access/PageShell';
import { HttpUtil, IntlUtil, SizeFormatter } from '@/utils';
import { defaultMapping, inboundsFor } from './importMapping';

type RemoteKind = 'marzban' | 'pasarguard' | 'remnawave';

export default function ImportPage() {
  const { t } = useTranslation();
  const { datepicker } = useDatepicker();
  const inbounds = useInboundOptions();
  const [mode, setMode] = useState<'file' | 'remote'>('file');
  const [kind, setKind] = useState<RemoteKind>('marzban');
  const [remote, setRemote] = useState({
    url: '',
    username: '',
    password: '',
    token: '',
    secret: '',
  });
  const [reading, setReading] = useState(false);
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [mapping, setMapping] = useState<Record<string, number[]>>({});
  const [fallback, setFallback] = useState<number[]>([]);
  const [keepLinks, setKeepLinks] = useState(true);
  const [applying, setApplying] = useState(false);
  const [done, setDone] = useState<ImportApplyResult | null>(null);

  const all = useMemo(() => inbounds.data ?? [], [inbounds.data]);
  const optionsOf = (list: typeof all) =>
    list.map((ib) => ({
      value: ib.id,
      label: `${ib.remark || ib.tag} · ${ib.protocol}:${ib.port}`,
    }));

  function previewed(p: ImportPreview | undefined | null) {
    if (!p) return;
    setPreview(p);
    setMapping(defaultMapping(p.protocols, all));
    setFallback([]);
    setDone(null);
  }

  async function readFile(file: File) {
    setReading(true);
    try {
      const form = new FormData();
      form.append('file', file);
      const msg = await HttpUtil.post<ImportPreview>('/panel/api/import/file', form);
      if (msg?.success) previewed(msg.obj);
    } finally {
      setReading(false);
    }
  }

  async function readRemote() {
    setReading(true);
    try {
      const msg = await HttpUtil.post<ImportPreview>(
        '/panel/api/import/remote',
        { kind, ...remote },
        { headers: { 'Content-Type': 'application/json' } },
      );
      if (msg?.success) previewed(msg.obj);
    } finally {
      setReading(false);
    }
  }

  async function apply() {
    if (!preview) return;
    setApplying(true);
    try {
      const msg = await HttpUtil.post<ImportApplyResult>(
        '/panel/api/import/apply',
        { id: preview.id, inbounds: mapping, fallback, keepLinks },
        { headers: { 'Content-Type': 'application/json' } },
      );
      if (msg?.success && msg.obj) setDone(msg.obj);
    } finally {
      setApplying(false);
    }
  }

  const mapped = Object.values(mapping).some((ids) => ids.length > 0) || fallback.length > 0;
  const step = done ? 2 : preview ? 1 : 0;

  const sampleColumns = [
    {
      title: t('pages.import.user'),
      key: 'u',
      render: (_: unknown, u: ImportUserPreview) => (
        <Space size={4}>
          <span>{u.username}</span>
          {!u.enabled && <Tag>{t('disabled')}</Tag>}
          {u.exists && <Tag color="orange">{t('pages.import.exists')}</Tag>}
        </Space>
      ),
    },
    {
      title: t('pages.import.traffic'),
      key: 't',
      render: (_: unknown, u: ImportUserPreview) =>
        `${SizeFormatter.sizeFormat(u.used)} / ${u.dataLimit ? SizeFormatter.sizeFormat(u.dataLimit) : '∞'}`,
    },
    {
      title: t('pages.import.expires'),
      key: 'e',
      render: (_: unknown, u: ImportUserPreview) =>
        u.expire ? IntlUtil.formatDate(u.expire, datepicker) : t('pages.import.never'),
    },
    {
      title: t('pages.import.protocols'),
      key: 'p',
      render: (_: unknown, u: ImportUserPreview) => (
        <Space size={[4, 4]} wrap>
          {u.protocols.map((p) => (
            <Tag key={p} bordered={false}>
              {p}
            </Tag>
          ))}
        </Space>
      ),
    },
  ];

  return (
    <PageShell className="import-page">
      <Card title={t('menu.import')}>
        <Typography.Paragraph type="secondary">{t('pages.import.intro')}</Typography.Paragraph>
        <Steps
          size="small"
          current={step}
          style={{ marginBottom: 24 }}
          items={[
            { title: t('pages.import.stepRead') },
            { title: t('pages.import.stepMap') },
            { title: t('pages.import.stepDone') },
          ]}
        />

        {step === 0 && (
          <>
            <Segmented
              value={mode}
              onChange={(v) => setMode(v as 'file' | 'remote')}
              style={{ marginBottom: 16 }}
              options={[
                { value: 'file', label: t('pages.import.fromFile') },
                { value: 'remote', label: t('pages.import.fromApi') },
              ]}
            />
            {mode === 'file' ? (
              <>
                <Upload.Dragger
                  accept=".db,.sqlite,.sqlite3,.json"
                  multiple={false}
                  showUploadList={false}
                  disabled={reading}
                  beforeUpload={(file) => {
                    void readFile(file);
                    return false;
                  }}
                >
                  <p className="ant-upload-drag-icon">
                    <InboxOutlined aria-hidden />
                  </p>
                  <p className="ant-upload-text">{t('pages.import.drop')}</p>
                  <p className="ant-upload-hint">{t('pages.import.dropHint')}</p>
                </Upload.Dragger>
                <Typography.Paragraph type="secondary" style={{ marginTop: 12, fontSize: 12 }}>
                  {t('pages.import.whereFiles')}
                </Typography.Paragraph>
              </>
            ) : (
              <Form layout="vertical" component="div" style={{ maxWidth: 560 }}>
                <Form.Item label={t('pages.import.panel')} htmlFor="imp-kind">
                  <Select
                    id="imp-kind"
                    value={kind}
                    onChange={setKind}
                    options={[
                      { value: 'marzban', label: 'Marzban' },
                      { value: 'pasarguard', label: 'PasarGuard' },
                      { value: 'remnawave', label: 'Remnawave' },
                    ]}
                  />
                </Form.Item>
                <Form.Item
                  label={t('pages.import.url')}
                  htmlFor="imp-url"
                  tooltip={t('pages.import.urlHint')}
                >
                  <Input
                    id="imp-url"
                    dir="ltr"
                    value={remote.url}
                    placeholder="https://old.example.com:8000"
                    onChange={(e) => setRemote({ ...remote, url: e.target.value })}
                  />
                </Form.Item>
                {kind === 'remnawave' ? (
                  <Form.Item label={t('pages.import.apiToken')} htmlFor="imp-token">
                    <Input.Password
                      id="imp-token"
                      value={remote.token}
                      onChange={(e) => setRemote({ ...remote, token: e.target.value })}
                    />
                  </Form.Item>
                ) : (
                  <>
                    <Row gutter={12}>
                      <Col xs={24} md={12}>
                        <Form.Item label={t('username')} htmlFor="imp-user">
                          <Input
                            id="imp-user"
                            value={remote.username}
                            onChange={(e) => setRemote({ ...remote, username: e.target.value })}
                          />
                        </Form.Item>
                      </Col>
                      <Col xs={24} md={12}>
                        <Form.Item label={t('password')} htmlFor="imp-pass">
                          <Input.Password
                            id="imp-pass"
                            value={remote.password}
                            onChange={(e) => setRemote({ ...remote, password: e.target.value })}
                          />
                        </Form.Item>
                      </Col>
                    </Row>
                    <Form.Item
                      label={t('pages.import.secret')}
                      htmlFor="imp-secret"
                      tooltip={t('pages.import.secretHint')}
                    >
                      <Input.Password
                        id="imp-secret"
                        value={remote.secret}
                        onChange={(e) => setRemote({ ...remote, secret: e.target.value })}
                      />
                    </Form.Item>
                  </>
                )}
                <Button
                  type="primary"
                  loading={reading}
                  disabled={!remote.url}
                  onClick={readRemote}
                >
                  {t('pages.import.read')}
                </Button>
              </Form>
            )}
          </>
        )}

        {step === 1 && preview && (
          <Space direction="vertical" size={16} style={{ width: '100%' }}>
            <Descriptions size="small" bordered column={{ xs: 1, md: 3 }}>
              <Descriptions.Item label={t('pages.import.source')}>
                <Tag color="blue">{preview.source}</Tag>
              </Descriptions.Item>
              <Descriptions.Item label={t('pages.import.users')}>{preview.users}</Descriptions.Item>
              <Descriptions.Item label={t('pages.import.alreadyHere')}>
                {preview.existing}
              </Descriptions.Item>
            </Descriptions>
            {preview.warnings.map((w) => (
              <Alert key={w} type="warning" showIcon title={w} />
            ))}
            <Typography.Title level={5} style={{ margin: 0 }}>
              {t('pages.import.whereTo')}
            </Typography.Title>
            {Object.entries(preview.protocols).map(([proto, count]) => (
              <Form.Item
                key={proto}
                label={t('pages.import.protocolUsers', { protocol: proto, count })}
                htmlFor={`imp-map-${proto}`}
                style={{ marginBottom: 8 }}
              >
                <Select
                  id={`imp-map-${proto}`}
                  mode="multiple"
                  allowClear
                  tagRender={selectTagRender}
                  value={mapping[proto] ?? []}
                  onChange={(ids) => setMapping({ ...mapping, [proto]: ids })}
                  placeholder={t('pages.import.skipProtocol')}
                  options={optionsOf(inboundsFor(proto, all))}
                />
              </Form.Item>
            ))}
            <Form.Item
              label={t('pages.import.fallback')}
              htmlFor="imp-fallback"
              tooltip={t('pages.import.fallbackHint')}
              style={{ marginBottom: 8 }}
            >
              <Select
                id="imp-fallback"
                mode="multiple"
                allowClear
                tagRender={selectTagRender}
                value={fallback}
                onChange={setFallback}
                options={optionsOf(all)}
              />
            </Form.Item>
            <Space align="center">
              <Switch id="imp-keep" checked={keepLinks} onChange={setKeepLinks} />
              <label htmlFor="imp-keep">{t('pages.import.keepLinks')}</label>
            </Space>
            {keepLinks && (
              <Typography.Paragraph type="secondary" style={{ fontSize: 12, margin: 0 }}>
                {t('pages.import.keepLinksHint')}
              </Typography.Paragraph>
            )}
            <Table<ImportUserPreview>
              size="small"
              rowKey="username"
              dataSource={preview.sample}
              columns={sampleColumns}
              pagination={{ pageSize: 10, hideOnSinglePage: true }}
              scroll={{ x: 'max-content' }}
              title={() =>
                t('pages.import.sample', { shown: preview.sample.length, total: preview.users })
              }
            />
            <Space>
              <Button onClick={() => setPreview(null)}>{t('pages.import.back')}</Button>
              <Button type="primary" loading={applying} disabled={!mapped} onClick={apply}>
                {t('pages.import.apply', { count: preview.users - preview.existing })}
              </Button>
            </Space>
          </Space>
        )}

        {step === 2 && done && (
          <Result
            status={done.skipped.length > 0 ? 'warning' : 'success'}
            title={t('pages.import.created', { count: done.created })}
            subTitle={keepLinks ? t('pages.import.aliases', { count: done.aliases }) : undefined}
            extra={
              <Button
                onClick={() => {
                  setPreview(null);
                  setDone(null);
                }}
              >
                {t('pages.import.another')}
              </Button>
            }
          >
            {done.skipped.length > 0 && (
              <Table
                size="small"
                rowKey="email"
                dataSource={done.skipped}
                pagination={{ pageSize: 10, hideOnSinglePage: true }}
                columns={[
                  { title: t('pages.import.user'), dataIndex: 'email', key: 'email' },
                  { title: t('pages.import.reason'), dataIndex: 'reason', key: 'reason' },
                ]}
              />
            )}
          </Result>
        )}
      </Card>
    </PageShell>
  );
}
