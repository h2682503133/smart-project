import { Leaf } from 'lucide-react';
import { FormEvent } from 'react';
import type { RegistrationRole } from '../types';

export type AuthMode = 'login' | 'register';

export type AuthCredentials = {
  username: string;
  password: string;
  mobile: string;
  role: RegistrationRole;
};

export const defaultCredentials: AuthCredentials = {
  username: '',
  password: '',
  mobile: '',
  role: 'FARMER'
};

type LoginPageProps = {
  authMode: AuthMode;
  credentials: AuthCredentials;
  busy: boolean;
  notice: string;
  onAuthModeChange: (mode: AuthMode) => void;
  onCredentialsChange: (credentials: AuthCredentials) => void;
  onSubmit: (event: FormEvent) => void;
};

export function LoginPage({
  authMode,
  credentials,
  busy,
  notice,
  onAuthModeChange,
  onCredentialsChange,
  onSubmit
}: LoginPageProps) {
  const errorField = notice ? loginErrorField(notice, authMode) : null;
  const connectionError = notice && !errorField ? notice : '';

  return (
    <main className="page-shell auth-shell">
      <section className="intro">
        <p className="eyebrow">Smart Agriculture</p>
        <h1>移动端 A3 实时看板</h1>
        <p>偏白背景、高对比、扁平化、适老化，按现有 Go 后端接口直连。</p>
      </section>
      <section className="phone auth-phone">
        <div className="brand-row">
          <div>
            <strong>智慧农田</strong>
            <span>张家湾温室 A3</span>
          </div>
          <Leaf className="brand-icon" size={28} aria-hidden="true" />
        </div>
        <form className="auth-card" onSubmit={onSubmit}>
          <h2>{authMode === 'login' ? '登录控制台' : '注册账号'}</h2>
          <label>
            账号
            <input
              value={credentials.username}
              onChange={(event) => onCredentialsChange({ ...credentials, username: event.target.value })}
              placeholder="请输入用户名"
              className={errorField === 'username' ? 'input-invalid' : undefined}
              aria-invalid={errorField === 'username'}
              aria-describedby={errorField === 'username' ? 'auth-username-error' : undefined}
            />
            {errorField === 'username' && <p className="field-error" id="auth-username-error" role="alert">{notice}</p>}
          </label>
          {authMode === 'register' && (
            <>
              <label>
                手机号
                <input
                  value={credentials.mobile}
                  onChange={(event) => onCredentialsChange({ ...credentials, mobile: event.target.value })}
                  placeholder="请输入手机号"
                  className={errorField === 'mobile' ? 'input-invalid' : undefined}
                  aria-invalid={errorField === 'mobile'}
                  aria-describedby={errorField === 'mobile' ? 'auth-mobile-error' : undefined}
                />
                {errorField === 'mobile' && <p className="field-error" id="auth-mobile-error" role="alert">{notice}</p>}
              </label>
              <fieldset className="auth-role-field">
                <legend>注册身份</legend>
                <label className={credentials.role === 'FARMER' ? 'role-option selected' : 'role-option'}>
                  <input
                    type="radio"
                    name="registration-role"
                    value="FARMER"
                    checked={credentials.role === 'FARMER'}
                    onChange={() => onCredentialsChange({ ...credentials, role: 'FARMER' })}
                  />
                  <span>农户</span>
                  <small>管理地块与种植</small>
                </label>
                <label className={credentials.role === 'CUSTOMER' ? 'role-option selected' : 'role-option'}>
                  <input
                    type="radio"
                    name="registration-role"
                    value="CUSTOMER"
                    checked={credentials.role === 'CUSTOMER'}
                    onChange={() => onCredentialsChange({ ...credentials, role: 'CUSTOMER' })}
                  />
                  <span>顾客</span>
                  <small>浏览农产品并提交采购意向</small>
                </label>
              </fieldset>
            </>
          )}
          <label>
            密码
            <input
              type="password"
              value={credentials.password}
              onChange={(event) => onCredentialsChange({ ...credentials, password: event.target.value })}
              placeholder="请输入密码"
              className={errorField === 'password' ? 'input-invalid' : undefined}
              aria-invalid={errorField === 'password'}
              aria-describedby={errorField === 'password' ? 'auth-password-error' : undefined}
            />
            {errorField === 'password' && <p className="field-error" id="auth-password-error" role="alert">{notice}</p>}
          </label>
          {connectionError && <p className="auth-connection-error" role="alert">{connectionError}</p>}
          <button className="primary-button" disabled={busy}>
            {busy ? '处理中...' : authMode === 'login' ? '进入看板' : '注册并登录'}
          </button>
          <button
            className="text-button"
            type="button"
            onClick={() => onAuthModeChange(authMode === 'login' ? 'register' : 'login')}
          >
            {authMode === 'login' ? '没有账号，去注册' : '已有账号，去登录'}
          </button>
        </form>
      </section>
    </main>
  );
}

function loginErrorField(notice: string, authMode: AuthMode): 'username' | 'password' | 'mobile' | null {
  const normalized = notice.toLowerCase();
  if (/服务|api|连接|network|fetch|localhost|不可达|超时|timeout/.test(normalized)) return null;
  if (authMode === 'register' && /手机|mobile|phone/.test(normalized)) return 'mobile';
  if (/(账号|用户名|用户|username)/.test(normalized) && !/(密码|password)/.test(normalized)) return 'username';
  return 'password';
}
