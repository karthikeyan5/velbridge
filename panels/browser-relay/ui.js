import { html, useState, useEffect, useRef } from '/core/vendor/preact-htm.js';

function detectPlatform() {
  const ua = navigator.userAgent || '';
  const pl = navigator.platform || '';
  if (/Android|iPhone|iPad|iPod/i.test(ua)) return null;
  if (/Win/i.test(pl)) return 'windows';
  if (/Mac/i.test(pl)) return 'mac';
  if (/Linux/i.test(pl)) return 'linux';
  return null;
}

const PLATFORM_LABELS = { linux: 'Linux', mac: 'Mac', windows: 'Windows' };
const ALL_PLATFORMS = ['linux', 'mac', 'windows'];

const STATE_CONFIG = {
  disconnected: { color: '#666', icon: '⚫', label: 'Not connected' },
  connected:    { color: '#22c55e', icon: '🟢', label: 'Browser connected' },
  agent_active: { color: '#0af', icon: '🤖', label: 'AI controlling browser' },
};

function timeAgo(dateStr) {
  if (!dateStr) return '';
  const diff = Date.now() - new Date(dateStr).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return mins + 'm ago';
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return hrs + 'h ' + (mins % 60) + 'm';
  return Math.floor(hrs / 24) + 'd';
}

function truncUrl(url, max) {
  if (!url) return '';
  try { url = new URL(url).hostname + new URL(url).pathname; } catch(e) {}
  return url.length > max ? url.slice(0, max) + '…' : url;
}

export default function BrowserRelayPanel({ data, error, connected, lastUpdate, api, config, cls }) {
  const [status, setStatus] = useState({ state: 'disconnected', targets: [] });
  const [showGuide, setShowGuide] = useState(false);
  const [showOther, setShowOther] = useState(false);
  const pollRef = useRef(null);

  useEffect(() => {
    const poll = async () => {
      try {
        const resp = await fetch('/relay/status');
        if (resp.ok) {
          const s = await resp.json();
          // Also try to get targets
          try {
            const tr = await fetch('/relay/json');
            if (tr.ok) s.targets = await tr.json();
          } catch(e) {}
          setStatus(s);
        }
      } catch(e) {}
    };
    poll();
    pollRef.current = setInterval(poll, 5000);
    return () => clearInterval(pollRef.current);
  }, []);

  const cfg = STATE_CONFIG[status.state] || STATE_CONFIG.disconnected;
  const platform = detectPlatform();
  const others = ALL_PLATFORMS.filter(p => p !== platform);
  const targets = (status.targets || []).filter(t => t.type === 'page' && !t.url?.startsWith('devtools://'));

  const sinceText = status.connectedSince
    ? timeAgo(status.connectedSince)
    : '';

  const s = {
    wrap: { padding: '20px', fontFamily: '-apple-system, system-ui, sans-serif', color: '#e0e0e0' },
    title: { fontSize: '15px', fontWeight: 700, color: '#e0e0e0', marginBottom: '4px', display: 'flex', alignItems: 'center', gap: '8px' },
    subtitle: { fontSize: '12px', color: '#888', marginBottom: '16px', lineHeight: '1.5' },
    statusRow: { display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '8px' },
    dot: { width: '10px', height: '10px', borderRadius: '50%', background: cfg.color, display: 'inline-block', boxShadow: status.state !== 'disconnected' ? `0 0 8px ${cfg.color}` : 'none', flexShrink: 0 },
    statusLabel: { fontSize: '14px', fontWeight: 600, color: cfg.color },
    meta: { fontSize: '11px', color: '#777', marginBottom: '14px' },
    // Tabs list
    tabsBox: { background: '#1a1a24', borderRadius: '8px', padding: '10px 12px', marginBottom: '14px', border: '1px solid #2a2a35' },
    tabsTitle: { fontSize: '11px', fontWeight: 700, color: '#888', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: '8px' },
    tabRow: { display: 'flex', alignItems: 'center', gap: '8px', padding: '5px 0', borderBottom: '1px solid #2a2a35' },
    tabFavicon: { width: '14px', height: '14px', borderRadius: '2px', flexShrink: 0 },
    tabTitle: { fontSize: '12px', color: '#ccc', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
    tabUrl: { fontSize: '10px', color: '#666', fontFamily: 'monospace' },
    // Stats row
    statsRow: { display: 'flex', gap: '16px', marginBottom: '14px' },
    stat: { display: 'flex', flexDirection: 'column', alignItems: 'center' },
    statNum: { fontSize: '18px', fontWeight: 700, color: '#0af' },
    statLabel: { fontSize: '10px', color: '#777', textTransform: 'uppercase', letterSpacing: '0.5px' },
    // Steps
    stepsBox: { background: '#1a1a24', borderRadius: '8px', padding: '14px', marginBottom: '14px', border: '1px solid #2a2a35' },
    stepRow: { display: 'flex', gap: '10px', marginBottom: '10px', alignItems: 'flex-start' },
    stepNum: { width: '22px', height: '22px', borderRadius: '6px', background: 'rgba(0,170,255,0.12)', color: '#0af', fontSize: '12px', fontWeight: 700, display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 },
    stepText: { fontSize: '12px', color: '#bbb', lineHeight: '1.5', paddingTop: '2px' },
    stepBold: { color: '#e0e0e0', fontWeight: 600 },
    btn: { padding: '8px 16px', border: '1px solid #0af', borderRadius: '6px', background: 'rgba(0,170,255,0.1)', color: '#0af', cursor: 'pointer', fontSize: '12px', fontWeight: 600, transition: 'all 0.2s' },
    btnRow: { display: 'flex', gap: '10px', alignItems: 'center', flexWrap: 'wrap', marginBottom: '14px' },
    guideToggle: { color: '#0af', fontSize: '12px', cursor: 'pointer', opacity: 0.8, userSelect: 'none', display: 'inline-flex', alignItems: 'center', gap: '4px' },
    useCaseGrid: { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px', marginTop: '12px' },
    useCaseCard: { background: '#1a1a24', borderRadius: '8px', padding: '10px 12px', border: '1px solid #2a2a35' },
    useCaseEmoji: { fontSize: '16px', marginBottom: '4px' },
    useCaseTitle: { fontSize: '12px', fontWeight: 600, color: '#ddd', marginBottom: '3px' },
    useCaseDesc: { fontSize: '11px', color: '#888', lineHeight: '1.4' },
    otherLink: { color: '#0af', fontSize: '11px', cursor: 'pointer', textDecoration: 'none', opacity: 0.6 },
  };

  const USE_CASES = [
    { emoji: '🔍', title: 'Web Research', desc: 'Research competitors, suppliers, market data' },
    { emoji: '📧', title: 'Email & Messages', desc: 'Check emails, draft replies, scan updates' },
    { emoji: '🛒', title: 'Shopping', desc: 'Compare prices across sites' },
    { emoji: '📊', title: 'Data Collection', desc: 'Scrape listings, pull specs, build sheets' },
  ];

  return html`
    <div style=${s.wrap}>
      <div style=${s.title}>🌐 Browser Remote Control</div>
      <div style=${s.subtitle}>Let your AI control a browser on your computer — from anywhere.</div>

      <!-- Status -->
      <div style=${s.statusRow}>
        <span style=${s.dot}></span>
        <span style=${s.statusLabel}>${cfg.label}</span>
      </div>

      ${status.state !== 'disconnected' && html`
        <!-- Stats -->
        <div style=${s.statsRow}>
          <div style=${s.stat}>
            <div style=${s.statNum}>${targets.length}</div>
            <div style=${s.statLabel}>Tabs</div>
          </div>
          <div style=${s.stat}>
            <div style=${s.statNum}>${status.msgCount || 0}</div>
            <div style=${s.statLabel}>Actions</div>
          </div>
          <div style=${s.stat}>
            <div style=${s.statNum}>${sinceText || '—'}</div>
            <div style=${s.statLabel}>Uptime</div>
          </div>
        </div>

        <!-- Tabs list -->
        ${targets.length > 0 && html`
          <div style=${s.tabsBox}>
            <div style=${s.tabsTitle}>Open Tabs (${targets.length})</div>
            ${targets.map((t, i) => html`
              <div style=${{ ...s.tabRow, borderBottom: i === targets.length - 1 ? 'none' : s.tabRow.borderBottom }}>
                <img style=${s.tabFavicon} src=${'https://www.google.com/s2/favicons?domain=' + (t.url ? new URL(t.url).hostname : '') + '&sz=16'} onerror=${e => e.target.style.display='none'} />
                <div style=${{ flex: 1, overflow: 'hidden' }}>
                  <div style=${s.tabTitle}>${t.title || 'Untitled'}</div>
                  <div style=${s.tabUrl}>${truncUrl(t.url, 50)}</div>
                </div>
              </div>
            `)}
          </div>
        `}
      `}

      ${status.state === 'disconnected' && html`
        <div style=${s.stepsBox}>
          <div style=${s.stepRow}>
            <div style=${s.stepNum}>1</div>
            <div style=${s.stepText}><span style=${s.stepBold}>Download & run</span> the launcher on your computer</div>
          </div>
          <div style=${s.stepRow}>
            <div style=${s.stepNum}>2</div>
            <div style=${s.stepText}><span style=${s.stepBold}>Pair</span> — send the 6-digit code to Ram on Telegram</div>
          </div>
          <div style=${{ ...s.stepRow, marginBottom: 0 }}>
            <div style=${s.stepNum}>3</div>
            <div style=${s.stepText}><span style=${s.stepBold}>Done!</span> AI can now control the browser remotely</div>
          </div>
        </div>
      `}

      <!-- Download buttons -->
      <div style=${s.btnRow}>
        ${platform ? html`
          <button style=${s.btn} onclick=${() => window.open('/relay/download?platform=' + platform, '_blank')}>
            ⬇ Download for ${PLATFORM_LABELS[platform]}
          </button>
        ` : html`
          ${ALL_PLATFORMS.map(p => html`
            <button style=${s.btn} onclick=${() => window.open('/relay/download?platform=' + p, '_blank')}>
              ⬇ ${PLATFORM_LABELS[p]}
            </button>
          `)}
        `}
        ${platform && html`
          <span style=${s.guideToggle} onclick=${() => setShowOther(!showOther)}>
            ${showOther ? '▾' : '▸'} Other platforms
          </span>
        `}
      </div>
      ${showOther && platform && html`
        <div style=${{ display: 'flex', gap: '12px', marginBottom: '14px', marginTop: '-6px' }}>
          ${others.map(p => html`
            <a href=${'/relay/download?platform=' + p} style=${s.otherLink}>⬇ ${PLATFORM_LABELS[p]}</a>
          `)}
        </div>
      `}

      <div>
        <span style=${s.guideToggle} onclick=${() => setShowGuide(!showGuide)}>
          ${showGuide ? '▾' : '▸'} What can I do with this?
        </span>
      </div>

      ${showGuide && html`
        <div style=${s.useCaseGrid}>
          ${USE_CASES.map(uc => html`
            <div style=${s.useCaseCard}>
              <div style=${s.useCaseEmoji}>${uc.emoji}</div>
              <div style=${s.useCaseTitle}>${uc.title}</div>
              <div style=${s.useCaseDesc}>${uc.desc}</div>
            </div>
          `)}
        </div>
      `}
    </div>
  `;
}
