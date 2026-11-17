import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Form, Input, Modal, Select } from 'antd';

import { selectTagRender } from '@/components/form';

interface UploadCertModalProps {
  open: boolean;
  nodes: { id: number; name: string }[];
  onUpload: (payload: {
    name: string;
    cert: string;
    key: string;
    nodeIds: number[];
  }) => Promise<boolean>;
  onClose: () => void;
}

export default function UploadCertModal({ open, nodes, onUpload, onClose }: UploadCertModalProps) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [cert, setCert] = useState('');
  const [key, setKey] = useState('');
  const [nodeIds, setNodeIds] = useState<number[]>([]);
  const [saving, setSaving] = useState(false);

  async function submit() {
    setSaving(true);
    try {
      if (await onUpload({ name: name.trim(), cert, key, nodeIds })) {
        setName('');
        setCert('');
        setKey('');
        setNodeIds([]);
        onClose();
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open={open}
      onCancel={onClose}
      width={640}
      title={t('pages.certs.upload')}
      okText={t('pages.certs.upload')}
      okButtonProps={{ disabled: !cert.trim() || !key.trim() }}
      confirmLoading={saving}
      onOk={submit}
    >
      <Form layout="vertical" component="div">
        <Form.Item
          label={t('pages.certs.name')}
          tooltip={t('pages.certs.nameHint')}
          htmlFor="cert-up-name"
        >
          <Input
            id="cert-up-name"
            value={name}
            placeholder={t('pages.certs.nameAuto')}
            onChange={(e) => setName(e.target.value)}
          />
        </Form.Item>
        <Form.Item label={t('pages.certs.chain')} htmlFor="cert-up-chain">
          <Input.TextArea
            id="cert-up-chain"
            dir="ltr"
            rows={6}
            value={cert}
            placeholder="-----BEGIN CERTIFICATE-----"
            onChange={(e) => setCert(e.target.value)}
          />
        </Form.Item>
        <Form.Item label={t('pages.certs.key')} htmlFor="cert-up-key">
          <Input.TextArea
            id="cert-up-key"
            dir="ltr"
            rows={5}
            value={key}
            placeholder="-----BEGIN PRIVATE KEY-----"
            onChange={(e) => setKey(e.target.value)}
          />
        </Form.Item>
        {nodes.length > 0 && (
          <Form.Item
            label={t('pages.certs.nodes')}
            tooltip={t('pages.certs.nodesHint')}
            htmlFor="cert-up-nodes"
          >
            <Select
              id="cert-up-nodes"
              mode="multiple"
              allowClear
              tagRender={selectTagRender}
              value={nodeIds}
              onChange={setNodeIds}
              options={nodes.map((n) => ({ value: n.id, label: n.name }))}
            />
          </Form.Item>
        )}
      </Form>
    </Modal>
  );
}
