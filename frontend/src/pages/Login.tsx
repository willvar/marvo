import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Input, Button, App } from 'antd'
import { useAuthStore } from '../stores/auth'

export default function Login() {
  const [password, setPassword] = useState('')
  const [loading, setLoading] = useState(false)
  const navigate = useNavigate()
  const { message } = App.useApp()
  const login = useAuthStore((s) => s.login)

  async function handleLogin() {
    if (!password) return
    setLoading(true)
    try {
      await login(password)
      navigate('/')
    } catch {
      message.error('密码错误')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={styles.container}>
      <div style={styles.card}>
        <div style={styles.logo}>
          <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
            <polyline points="14 2 14 8 20 8"/>
            <line x1="16" y1="13" x2="8" y2="13"/>
            <line x1="16" y1="17" x2="8" y2="17"/>
            <polyline points="10 9 9 9 8 9"/>
          </svg>
        </div>
        <h1 style={styles.title}>Marvo</h1>
        <p style={styles.subtitle}>轻量笔记，专注记录</p>
        <div style={styles.form}>
          <Input.Password
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="请输入密码"
            size="large"
            onPressEnter={handleLogin}
          />
          <Button
            type="primary"
            block
            size="large"
            loading={loading}
            onClick={handleLogin}
          >
            进入
          </Button>
        </div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    height: '100vh',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    background: '#fafafa',
  },
  card: {
    width: 340,
    textAlign: 'center',
    padding: '48px 36px 36px',
    background: '#ffffff',
    borderRadius: 16,
    border: '1px solid #f0f0f2',
    boxShadow: '0 4px 24px rgba(0, 0, 0, 0.06)',
  },
  logo: {
    width: 56,
    height: 56,
    borderRadius: 14,
    background: '#4f46e5',
    color: '#fff',
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: 20,
  },
  title: {
    fontSize: 24,
    fontWeight: 700,
    color: '#1a1a1a',
    margin: '0 0 4px',
    letterSpacing: '-0.02em',
  },
  subtitle: {
    fontSize: 14,
    color: '#9ca3af',
    margin: '0 0 28px',
  },
  form: {
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
  },
}
