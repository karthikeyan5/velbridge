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

function formatBytes(bytes) {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
  return (bytes / 1048576).toFixed(1) + ' MB';
}

// ── Tab Badge Component ──
function TabBadge({ count }) {
  if (!count) return null;
  return html`<span style=${{
    background: '#0af', color: '#000', fontSize: '10px', fontWeight: 700,
    padding: '1px 6px', borderRadius: '10px', marginLeft: '6px'
  }}>${count}</span>`;
}

export default function BrowserRelayPanel({ data, error, connected, lastUpdate, api, config, cls }) {
  const [activeTab, setActiveTab] = useState('debug');
  const [v2Status, setV2Status] = useState(null);
  const [debugStatus, setDebugStatus] = useState({ state: 'disconnected', targets: [] });
  const [showGuide, setShowGuide] = useState(false);
  const [showOther, setShowOther] = useState(false);
  const [proxyDomain, setProxyDomain] = useState('');
  const [observeMode, setObserveMode] = useState('screenshot');
  const [observeLabel, setObserveLabel] = useState('');
  const [createdLink, setCreatedLink] = useState('');
  const pollRef = useRef(null);

  useEffect(() => {
    const poll = async () => {
      try {
        // Fetch combined v2 status
        const resp = await fetch('/bridge/status', { credentials: 'same-origin' });
        if (resp.ok) {
          const s = await resp.json();
          setV2Status(s);
          if (s.debug) setDebugStatus(s.debug);
        }
      } catch(e) {
        // Fallback to legacy status
        try {
          const resp = await fetch('/bridge/debug/status', { credentials: 'same-origin' });
          if (resp.ok) {
            const s = await resp.json();
            try { const tr = await fetch('/bridge/debug/json', { credentials: 'same-origin' }); if (tr.ok) s.targets = await tr.json(); } catch(e) {}
            setDebugStatus(s);
          }
        } catch(e) {}
      }
    };
    poll();
    pollRef.current = setInterval(poll, 5000);
    return () => clearInterval(pollRef.current);
  }, []);

  const cfg = STATE_CONFIG[debugStatus.state] || STATE_CONFIG.disconnected;
  const platform = detectPlatform();
  const others = ALL_PLATFORMS.filter(p => p !== platform);
  const targets = (debugStatus.targets || []).filter(t => t.type === 'page' && !t.url?.startsWith('devtools://'));
  const proxySessions = v2Status?.proxy_sessions || [];
  const observeSessions = v2Status?.observe_sessions || [];
  const observeHistory = v2Status?.observe_history || [];

  const s = {
    wrap: { padding: '20px', fontFamily: '-apple-system, system-ui, sans-serif', color: '#e0e0e0' },
    title: { fontSize: '15px', fontWeight: 700, color: '#e0e0e0', marginBottom: '4px', display: 'flex', alignItems: 'center', gap: '8px' },
    subtitle: { fontSize: '12px', color: '#888', marginBottom: '16px', lineHeight: '1.5' },
    tabs: { display: 'flex', gap: '4px', marginBottom: '16px', borderBottom: '1px solid #2a2a35', paddingBottom: '8px' },
    tab: { padding: '6px 14px', border: '1px solid transparent', borderRadius: '6px', background: 'transparent', color: '#888', cursor: 'pointer', fontSize: '12px', fontWeight: 600, transition: 'all 0.2s', display: 'flex', alignItems: 'center' },
    tabActive: { background: 'rgba(0,170,255,0.08)', borderColor: 'rgba(0,170,255,0.2)', color: '#0af' },
    statusRow: { display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '8px' },
    dot: { width: '10px', height: '10px', borderRadius: '50%', background: cfg.color, display: 'inline-block', boxShadow: debugStatus.state !== 'disconnected' ? `0 0 8px ${cfg.color}` : 'none', flexShrink: 0 },
    statusLabel: { fontSize: '14px', fontWeight: 600, color: cfg.color },
    tabsBox: { background: '#1a1a24', borderRadius: '8px', padding: '10px 12px', marginBottom: '14px', border: '1px solid #2a2a35' },
    tabsTitle: { fontSize: '11px', fontWeight: 700, color: '#888', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: '8px' },
    tabRow: { display: 'flex', alignItems: 'center', gap: '8px', padding: '5px 0', borderBottom: '1px solid #2a2a35' },
    tabFavicon: { width: '14px', height: '14px', borderRadius: '2px', flexShrink: 0 },
    tabTitle: { fontSize: '12px', color: '#ccc', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
    tabUrl: { fontSize: '10px', color: '#666', fontFamily: 'monospace' },
    statsRow: { display: 'flex', gap: '16px', marginBottom: '14px' },
    stat: { display: 'flex', flexDirection: 'column', alignItems: 'center' },
    statNum: { fontSize: '18px', fontWeight: 700, color: '#0af' },
    statLabel: { fontSize: '10px', color: '#777', textTransform: 'uppercase', letterSpacing: '0.5px' },
    stepsBox: { background: '#1a1a24', borderRadius: '8px', padding: '14px', marginBottom: '14px', border: '1px solid #2a2a35' },
    stepRow: { display: 'flex', gap: '10px', marginBottom: '10px', alignItems: 'flex-start' },
    stepNum: { width: '22px', height: '22px', borderRadius: '6px', background: 'rgba(0,170,255,0.12)', color: '#0af', fontSize: '12px', fontWeight: 700, display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 },
    stepText: { fontSize: '12px', color: '#bbb', lineHeight: '1.5', paddingTop: '2px' },
    stepBold: { color: '#e0e0e0', fontWeight: 600 },
    btn: { padding: '8px 16px', border: '1px solid #0af', borderRadius: '6px', background: 'rgba(0,170,255,0.1)', color: '#0af', cursor: 'pointer', fontSize: '12px', fontWeight: 600, transition: 'all 0.2s' },
    btnRow: { display: 'flex', gap: '10px', alignItems: 'center', flexWrap: 'wrap', marginBottom: '14px' },
    guideToggle: { color: '#0af', fontSize: '12px', cursor: 'pointer', opacity: 0.8, userSelect: 'none', display: 'inline-flex', alignItems: 'center', gap: '4px' },
    otherLink: { color: '#0af', fontSize: '11px', cursor: 'pointer', textDecoration: 'none', opacity: 0.6 },
    input: { padding: '8px 12px', border: '1px solid #2a2a35', borderRadius: '6px', background: '#1a1a24', color: '#ddd', fontSize: '13px', flex: 1, outline: 'none' },
    sessionCard: { background: '#1a1a24', borderRadius: '8px', padding: '10px 12px', marginBottom: '8px', border: '1px solid #2a2a35' },
    sessionRow: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' },
    sessionLabel: { fontSize: '12px', color: '#ccc', fontWeight: 600 },
    sessionMeta: { fontSize: '11px', color: '#666' },
    linkBox: { background: '#0a0a0f', border: '1px solid #2a2a35', borderRadius: '6px', padding: '8px 12px', marginTop: '8px', wordBreak: 'break-all', fontSize: '11px', color: '#0af', fontFamily: 'monospace', cursor: 'pointer' },
    radioRow: { display: 'flex', gap: '6px', marginBottom: '8px' },
    radioLabel: { padding: '4px 10px', border: '1px solid #2a2a35', borderRadius: '6px', fontSize: '11px', color: '#999', cursor: 'pointer' },
    radioLabelActive: { background: 'rgba(0,170,255,0.12)', borderColor: 'rgba(0,170,255,0.3)', color: '#0af' },
    historyRow: { display: 'flex', justifyContent: 'space-between', padding: '4px 0', borderBottom: '1px solid #1e1e2e', fontSize: '11px', color: '#888' },
  };

  const sinceText = debugStatus.connectedSince ? timeAgo(debugStatus.connectedSince) : '';

  // ── Proxy: Create link ──
  const createProxyLink = () => {
    if (!proxyDomain) return;
    const link = window.location.origin + '/bridge/proxy/' + proxyDomain.replace(/^https?:\/\//, '') + '/';
    setCreatedLink(link);
  };

  // ── Observe: Create session ──
  const createObserveSession = async () => {
    try {
      const resp = await fetch('/api/bridge/observe/sessions', {
        method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode: observeMode, label: observeLabel || 'Dashboard session', timeout: 300 })
      });
      const data = await resp.json();
      setCreatedLink(data.url);
    } catch(e) {
      setCreatedLink('Error: ' + e.message);
    }
  };

  const copyLink = () => {
    navigator.clipboard?.writeText(createdLink);
  };

  return html`
    <div style=${s.wrap}>
      <div style=${s.title}>🌉 VelBridge</div>
      <div style=${s.subtitle}>Debug, proxy, and observe — three ways to bridge your AI to any browser.</div>

      <!-- Tabs -->
      <div style=${s.tabs}>
        <button style=${{ ...s.tab, ...(activeTab === 'debug' ? s.tabActive : {}) }}
          onclick=${() => { setActiveTab('debug'); setCreatedLink(''); }}>
          🛠 Debug${html`<${TabBadge} count=${debugStatus.state !== 'disconnected' ? 1 : 0} />`}
        </button>
        <button style=${{ ...s.tab, ...(activeTab === 'proxy' ? s.tabActive : {}) }}
          onclick=${() => { setActiveTab('proxy'); setCreatedLink(''); }}>
          🔀 Proxy${html`<${TabBadge} count=${proxySessions.length} />`}
        </button>
        <button style=${{ ...s.tab, ...(activeTab === 'observe' ? s.tabActive : {}) }}
          onclick=${() => { setActiveTab('observe'); setCreatedLink(''); }}>
          👁️ Observe${html`<${TabBadge} count=${observeSessions.length} />`}
        </button>
      </div>

      <!-- ═══ DEBUG TAB ═══ -->
      ${activeTab === 'debug' && html`
        <div style=${s.statusRow}>
          <span style=${s.dot}></span>
          <span style=${s.statusLabel}>${cfg.label}</span>
        </div>

        ${debugStatus.state !== 'disconnected' && html`
          <div style=${s.statsRow}>
            <div style=${s.stat}><div style=${s.statNum}>${targets.length}</div><div style=${s.statLabel}>Tabs</div></div>
            <div style=${s.stat}><div style=${s.statNum}>${debugStatus.msgCount || 0}</div><div style=${s.statLabel}>Actions</div></div>
            <div style=${s.stat}><div style=${s.statNum}>${sinceText || '—'}</div><div style=${s.statLabel}>Uptime</div></div>
          </div>
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

        ${debugStatus.state === 'disconnected' && html`
          <div style=${s.stepsBox}>
            <div style=${s.stepRow}><div style=${s.stepNum}>1</div><div style=${s.stepText}><span style=${s.stepBold}>Download & run</span> the launcher on your computer</div></div>
            <div style=${s.stepRow}><div style=${s.stepNum}>2</div><div style=${s.stepText}><span style=${s.stepBold}>Pair</span> — send the 6-digit code to Ram on Telegram</div></div>
            <div style=${{ ...s.stepRow, marginBottom: 0 }}><div style=${s.stepNum}>3</div><div style=${s.stepText}><span style=${s.stepBold}>Done!</span> AI can now control the browser remotely</div></div>
          </div>
        `}

        <div style=${s.btnRow}>
          ${platform ? html`
            <button style=${s.btn} onclick=${() => window.open('/bridge/debug/download?platform=' + platform, '_blank')}>⬇ Download for ${PLATFORM_LABELS[platform]}</button>
          ` : html`
            ${ALL_PLATFORMS.map(p => html`<button style=${s.btn} onclick=${() => window.open('/bridge/debug/download?platform=' + p, '_blank')}>⬇ ${PLATFORM_LABELS[p]}</button>`)}
          `}
          ${platform && html`
            <span style=${s.guideToggle} onclick=${() => setShowOther(!showOther)}>${showOther ? '▾' : '▸'} Other</span>
          `}
        </div>
        ${showOther && platform && html`
          <div style=${{ display: 'flex', gap: '12px', marginBottom: '14px', marginTop: '-6px' }}>
            ${others.map(p => html`<a href=${'/bridge/debug/download?platform=' + p} style=${s.otherLink}>⬇ ${PLATFORM_LABELS[p]}</a>`)}
          </div>
        `}
      `}

      <!-- ═══ PROXY TAB ═══ -->
      ${activeTab === 'proxy' && html`
        <div style=${{ marginBottom: '16px' }}>
          <div style=${{ fontSize: '12px', color: '#888', marginBottom: '8px' }}>Enter a domain to create a proxied link:</div>
          <div style=${{ display: 'flex', gap: '8px' }}>
            <input style=${s.input} placeholder="example.com" value=${proxyDomain} onInput=${e => setProxyDomain(e.target.value)} onkeydown=${e => e.key === 'Enter' && createProxyLink()} />
            <button style=${s.btn} onclick=${createProxyLink}>Create Link</button>
          </div>
          ${createdLink && html`
            <div style=${s.linkBox} onclick=${copyLink} title="Click to copy">
              ${createdLink}
              <span style=${{ marginLeft: '8px', fontSize: '10px', color: '#666' }}>📋 click to copy</span>
            </div>
          `}
        </div>

        ${proxySessions.length > 0 && html`
          <div style=${s.tabsBox}>
            <div style=${s.tabsTitle}>Active Proxy Sessions (${proxySessions.length})</div>
            ${proxySessions.map(sess => html`
              <div style=${s.sessionCard}>
                <div style=${s.sessionRow}>
                  <span style=${s.sessionLabel}>🔀 ${sess.domain}</span>
                  <span style=${s.sessionMeta}>${timeAgo(sess.since)}</span>
                </div>
              </div>
            `)}
          </div>
        `}

        ${proxySessions.length === 0 && html`
          <div style=${{ textAlign: 'center', padding: '24px', color: '#555', fontSize: '13px' }}>
            No active proxy sessions. Create one above!
          </div>
        `}

        <div style=${{ fontSize: '11px', color: '#555', lineHeight: '1.6' }}>
          <strong>Proxy mode</strong> loads any website through your dashboard's server. URLs are rewritten, cookies are jar'd server-side, and JS is injected for console/network capture and screenshots. Best for static/server-rendered sites.
        </div>
      `}

      <!-- ═══ OBSERVE TAB ═══ -->
      ${activeTab === 'observe' && html`
        <div style=${{ marginBottom: '16px' }}>
          <div style=${{ fontSize: '12px', color: '#888', marginBottom: '8px' }}>Create an Observe link to share with a user:</div>
          <div style=${{ display: 'flex', gap: '8px', marginBottom: '8px' }}>
            <input style=${{ ...s.input, flex: 2 }} placeholder="Label (e.g. Router setup)" value=${observeLabel} onInput=${e => setObserveLabel(e.target.value)} />
          </div>
          <div style=${s.radioRow}>
            ${['text', 'screenshot', 'stream'].map(m => html`
              <button style=${{ ...s.radioLabel, ...(observeMode === m ? s.radioLabelActive : {}) }}
                onclick=${() => setObserveMode(m)}>
                ${m === 'text' ? '💬' : m === 'screenshot' ? '📸' : '📹'} ${m}
              </button>
            `)}
          </div>
          <button style=${s.btn} onclick=${createObserveSession}>Create Observe Link</button>
          ${createdLink && html`
            <div style=${s.linkBox} onclick=${copyLink} title="Click to copy">
              ${createdLink}
              <span style=${{ marginLeft: '8px', fontSize: '10px', color: '#666' }}>📋 click to copy</span>
            </div>
          `}
        </div>

        ${observeSessions.length > 0 && html`
          <div style=${s.tabsBox}>
            <div style=${s.tabsTitle}>Active Sessions (${observeSessions.length})</div>
            ${observeSessions.map(sess => html`
              <div style=${s.sessionCard}>
                <div style=${s.sessionRow}>
                  <span style=${s.sessionLabel}>${sess.mode === 'stream' ? '📹' : sess.mode === 'screenshot' ? '📸' : '💬'} ${sess.label || sess.id}</span>
                  <span style=${{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                    <span style=${{ width: '6px', height: '6px', borderRadius: '50%', background: sess.user_connected ? '#22c55e' : '#ef4444' }}></span>
                    <span style=${s.sessionMeta}>${timeAgo(sess.since)}</span>
                  </span>
                </div>
                <div style=${{ fontSize: '11px', color: '#666', marginTop: '4px' }}>
                  📸 ${sess.screenshot_count} screenshots • ${formatBytes(sess.data_transferred)} transferred
                  ${sess.user_connected ? ' • 👤 User online' : ''}
                  ${sess.agent_connected ? ' • 🤖 Agent online' : ''}
                </div>
              </div>
            `)}
          </div>
        `}

        ${observeHistory.length > 0 && html`
          <div style=${s.tabsBox}>
            <div style=${s.tabsTitle}>Recent History</div>
            ${observeHistory.map(h => html`
              <div style=${s.historyRow}>
                <span>${h.label || h.id}</span>
                <span>${timeAgo(h.since)}</span>
              </div>
            `)}
          </div>
        `}

        ${observeSessions.length === 0 && observeHistory.length === 0 && html`
          <div style=${{ textAlign: 'center', padding: '24px', color: '#555', fontSize: '13px' }}>
            No observe sessions yet. Create one above!
          </div>
        `}

        <div style=${{ fontSize: '11px', color: '#555', lineHeight: '1.6' }}>
          <strong>Observe mode</strong> lets you see a user's screen in real-time. Start with text, upgrade to screenshots or live streaming. The user controls what's shared.
        </div>
      `}

      <!-- ═══ VISUAL DIFF TEASER ═══ -->
      <div style=${{ borderTop: '1px solid #2a2a35', marginTop: '20px', paddingTop: '16px', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div>
          <div style=${{ fontSize: '13px', fontWeight: 600, color: '#ccc', marginBottom: '3px' }}>✨ Visual Diff</div>
          <div style=${{ fontSize: '11px', color: '#666' }}>Compare any two pages visually</div>
        </div>
        <a href="/bridge/diff/" target="_blank" rel="noopener"
          style=${{ padding: '6px 14px', border: '1px solid #c9a84c', borderRadius: '6px', background: 'rgba(201,168,76,0.08)', color: '#c9a84c', cursor: 'pointer', fontSize: '11px', fontWeight: 600, textDecoration: 'none', whiteSpace: 'nowrap', transition: 'all 0.2s' }}>
          Open Diff →
        </a>
      </div>
    </div>
  `;
}
