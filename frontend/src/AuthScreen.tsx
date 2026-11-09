import { useState, type FormEvent } from 'react';
import './auth.css';

interface AuthScreenProps {
  username?: string;
  mustChangePassword: boolean;
  initialError?: string;
  notice?: string;
  onLogin: (username: string, password: string) => Promise<void>;
  onChangePassword: (currentPassword: string, newPassword: string) => Promise<void>;
}

export default function AuthScreen({ username: initialUsername = '', mustChangePassword, initialError = '', notice = '', onLogin, onChangePassword }: AuthScreenProps) {
  const [username, setUsername] = useState(initialUsername);
  const [password, setPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState(initialError);
  const [pending, setPending] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending) return;
    if (mustChangePassword && newPassword !== confirmPassword) {
      setError('两次输入的新密码不一致。');
      return;
    }
    setPending(true);
    setError('');
    try {
      if (mustChangePassword) await onChangePassword(password, newPassword);
      else await onLogin(username.trim(), password);
      setPassword('');
      setNewPassword('');
      setConfirmPassword('');
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '认证请求失败，请重试。');
    } finally {
      setPending(false);
    }
  }

  return (
    <main className="auth-screen">
      <section className="auth-card" aria-labelledby="auth-title">
        <div className="auth-brand"><span className="auth-mark">cc</span><span>cc-web</span></div>
        <p className="auth-eyebrow">安全工作区</p>
        <h1 id="auth-title">{mustChangePassword ? '更新你的密码' : '欢迎回来'}</h1>
        <p className="auth-description">{mustChangePassword ? '首次登录需要先设置新密码，完成后才能进入工作台。' : '使用管理员账号登录以访问工作台。'}</p>
        <form className="auth-form" onSubmit={submit}>
          {!mustChangePassword && <label>账号<input autoComplete="username" name="username" value={username} onChange={(event) => setUsername(event.target.value)} required autoFocus /></label>}
          {mustChangePassword && <div className="auth-identity"><span>当前账号</span><strong>{initialUsername}</strong></div>}
          <label>{mustChangePassword ? '当前密码' : '密码'}<input autoComplete={mustChangePassword ? 'current-password' : 'current-password'} name="password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} required autoFocus={mustChangePassword} /></label>
          {mustChangePassword && <>
            <label>新密码<input autoComplete="new-password" name="new-password" type="password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} minLength={12} maxLength={128} required /></label>
            <p className="auth-hint">新密码需为 12–128 个 UTF-8 字节。密码仅提交至服务端，不会保存在浏览器存储中。</p>
            <label>确认新密码<input autoComplete="new-password" name="confirm-password" type="password" value={confirmPassword} onChange={(event) => setConfirmPassword(event.target.value)} minLength={12} maxLength={128} required /></label>
          </>}
          {notice && <p className="auth-notice" role="status">{notice}</p>}
          {error && <p className="auth-error" role="alert">{error}</p>}
          <button className="auth-submit" type="submit" disabled={pending}>{pending ? '请稍候…' : mustChangePassword ? '更新密码并继续' : '登录'}</button>
        </form>
        <p className="auth-security-note">会话使用安全 Cookie 管理；登录凭据不会写入本地存储。</p>
      </section>
    </main>
  );
}
