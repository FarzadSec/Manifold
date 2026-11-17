import { useTranslation } from 'react-i18next';
import { Button } from 'antd';
import { CopyOutlined, KeyOutlined, TeamOutlined, UserOutlined } from '@ant-design/icons';

export interface SubAccount {
  password?: string;
  limitIp?: number;
  userLimit?: number;
}

interface SubAccountLineProps {
  username: string;
  users: number;
  password?: string;
  onCopy: (value: string, toast?: string) => void;
}

// Which account a service belongs to and how many may use it at once; a tunnel's app also asks for the password.
export default function SubAccountLine({ username, users, password, onCopy }: SubAccountLineProps) {
  const { t } = useTranslation();
  return (
    <div className="sub-account" data-testid="sub-account">
      <span className="sub-account-item">
        <UserOutlined aria-hidden />
        {t('username')}:<bdi className="sub-account-value">{username}</bdi>
      </span>
      {password && (
        <span className="sub-account-item">
          <KeyOutlined aria-hidden />
          {t('password')}:<bdi className="sub-account-value">{password}</bdi>
          <Button
            size="small"
            type="text"
            icon={<CopyOutlined aria-hidden />}
            aria-label={t('copy')}
            onClick={() => onCopy(password)}
          />
        </span>
      )}
      <span className="sub-account-item">
        <TeamOutlined aria-hidden />
        {t('subscription.usersAtOnce')}:
        <bdi className="sub-account-value">{users > 0 ? users : '∞'}</bdi>
      </span>
    </div>
  );
}
