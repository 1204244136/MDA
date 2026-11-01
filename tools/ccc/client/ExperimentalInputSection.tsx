import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { invoke } from '@tauri-apps/api/core';
import { FlaskConical } from 'lucide-react';
import { SwitchButton } from '@/components/FormControls';
import { useAppStore } from '@/stores/appStore';
import { buildDesktopWindowControllerConfig } from '@/utils/controller';

export function ExperimentalInputSection() {
  const { t } = useTranslation();
  const [enabled, setEnabled] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [countdown, setCountdown] = useState(0);
  const sequence = useRef(0);
  const running = useAppStore((s) => Object.values(s.instanceTaskStatus).some((status) => status === 'Pending' || status === 'Running'));
  useEffect(() => {
    invoke<boolean>('mda_experimental_status').then(setEnabled).catch((error) => setMessage(String(error))).finally(() => setLoaded(true));
  }, []);
  useEffect(() => {
    const cancel = () => { sequence.current++; setCountdown(0); };
    window.addEventListener('blur', cancel);
    return () => { window.removeEventListener('blur', cancel); sequence.current++; };
  }, []);
  useEffect(() => { if (running) { sequence.current++; setCountdown(0); } }, [running]);
  const toggle = async (value: boolean) => {
    setBusy(true);
    try {
      await invoke('mda_set_experimental', { enabled: value });
      setEnabled(value);
      setMessage(t('experimentalInput.restart'));
      window.location.reload();
    } catch (error) { setMessage(String(error)); }
    finally { setBusy(false); }
  };
  const probe = async (backend: 'BBB' | 'CCC') => {
    const current = ++sequence.current;
    setBusy(true);
    setMessage('');
    try {
      for (let seconds = 3; seconds > 0; seconds--) {
        setCountdown(seconds);
        await new Promise((resolve) => setTimeout(resolve, 1000));
        if (sequence.current !== current) { setMessage(t('experimentalInput.cancelled')); return; }
      }
      setCountdown(0);
      const controllers = useAppStore.getState().projectInterface?.controller ?? [];
      const defaultController = controllers.find((controller) => controller.name === 'Win32-Window') ??
        controllers.find((controller) => controller.type === 'Win32' && controller.name !== 'BBB' && controller.name !== 'CCC');
      const defaultConfig = buildDesktopWindowControllerConfig(defaultController, 0);
      setMessage(await invoke<string>('mda_probe_experimental', { backend, defaultConfig }));
    } catch (error) { setMessage(String(error)); }
    finally { setCountdown(0); setBusy(false); }
  };
  return (
    <section id="section-experimental-input" className="space-y-4 scroll-mt-4">
      <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider flex items-center gap-2">
        <FlaskConical className="w-4 h-4" />{t('experimentalInput.title')}
      </h2>
      <div className="bg-bg-secondary rounded-xl border border-border p-4 space-y-4">
        <div className="flex items-center justify-between gap-4">
          <div><div className="font-medium text-text-primary">{t('experimentalInput.enable')}</div>
            <p className="text-xs text-text-muted mt-1">{t('experimentalInput.description')}</p></div>
          <SwitchButton value={enabled} onChange={(value) => void toggle(value)} disabled={!loaded || busy || running} />
        </div>
        {enabled && <>
        <p className="text-xs text-text-muted">{t('experimentalInput.instructions')}</p>
        <div className="flex gap-2">
          {(['BBB', 'CCC'] as const).map((backend) => <button key={backend} type="button"
            disabled={!loaded || !enabled || busy || running} onClick={() => void probe(backend)}
            className="px-3 py-2 rounded-lg text-sm border border-border hover:bg-bg-hover disabled:opacity-50">
            {t('experimentalInput.test', { backend })}
          </button>)}
          {countdown > 0 && <button type="button" onClick={() => { sequence.current++; }}
            className="px-3 py-2 rounded-lg text-sm border border-border">{t('common.cancel')} ({countdown})</button>}
        </div>
        <p className="text-xs text-text-muted">{t(running ? 'experimentalInput.running' : 'experimentalInput.testHint')}</p>
        </>}
        {message && <p role="status" className="text-sm whitespace-pre-wrap break-words">{message}</p>}
      </div>
    </section>
  );
}
