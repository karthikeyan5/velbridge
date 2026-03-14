/**
 * VelBridge Proxy Mode — Recording Engine
 * Records user interactions and replays them with hybrid element matching.
 */
(function() {
  'use strict';

  var recording = [];
  var recordingActive = false;
  var recordStart = 0;

  // ── CSS Selector Generator ──
  function cssSelector(el) {
    if (!el || el === document.body) return 'body';
    if (el.id) return '#' + el.id;
    var parts = [];
    while (el && el !== document.body) {
      var selector = el.tagName.toLowerCase();
      if (el.id) { parts.unshift('#' + el.id); break; }
      var parent = el.parentElement;
      if (parent) {
        var siblings = Array.prototype.filter.call(parent.children, function(c) {
          return c.tagName === el.tagName;
        });
        if (siblings.length > 1) {
          var idx = Array.prototype.indexOf.call(parent.children, el) + 1;
          selector += ':nth-child(' + idx + ')';
        }
      }
      parts.unshift(selector);
      el = parent;
    }
    return parts.join(' > ');
  }

  function recordEvent(evt) {
    recording.push(Object.assign({}, evt, { timeMs: Date.now() - recordStart }));
  }

  // ── Event Handlers ──
  function onClickCapture(e) {
    if (!recordingActive) return;
    var el = e.target;
    recordEvent({
      type: 'click',
      text: (el.innerText || '').trim().slice(0, 50),
      tag: el.tagName,
      role: el.getAttribute('role'),
      name: el.getAttribute('name'),
      placeholder: el.getAttribute('placeholder'),
      selector: cssSelector(el),
      xPct: e.clientX / window.innerWidth,
      yPct: e.clientY / window.innerHeight
    });
  }

  function onInputCapture(e) {
    if (!recordingActive) return;
    var el = e.target;
    recordEvent({
      type: 'input',
      value: el.value,
      tag: el.tagName,
      name: el.getAttribute('name'),
      placeholder: el.getAttribute('placeholder'),
      selector: cssSelector(el)
    });
  }

  function onScrollCapture() {
    if (!recordingActive) return;
    recordEvent({ type: 'scroll', x: window.scrollX, y: window.scrollY });
  }

  // ── Start / Stop Recording ──
  window.__velStartRecording = function() {
    recording = [];
    recordingActive = true;
    recordStart = Date.now();
    document.addEventListener('click', onClickCapture, true);
    document.addEventListener('input', onInputCapture, true);
    document.addEventListener('scroll', onScrollCapture, true);
  };

  window.__velStopRecording = function() {
    recordingActive = false;
    document.removeEventListener('click', onClickCapture, true);
    document.removeEventListener('input', onInputCapture, true);
    document.removeEventListener('scroll', onScrollCapture, true);
    return recording;
  };

  // ── Hybrid Element Finder ──
  function findElement(evt) {
    // 1. Text + tag match (best for cross-site replay)
    if (evt.text && evt.tag) {
      var candidates = document.querySelectorAll(evt.tag);
      for (var i = 0; i < candidates.length; i++) {
        if ((candidates[i].innerText || '').trim().indexOf(evt.text) === 0) {
          return candidates[i];
        }
      }
    }

    // 2. Name attribute (form fields)
    if (evt.name) {
      var el = document.querySelector('[name="' + evt.name + '"]');
      if (el) return el;
    }

    // 3. Placeholder attribute (inputs)
    if (evt.placeholder) {
      var el = document.querySelector('[placeholder="' + evt.placeholder + '"]');
      if (el) return el;
    }

    // 4. Role attribute
    if (evt.role) {
      var el = document.querySelector('[role="' + evt.role + '"]');
      if (el) return el;
    }

    // 5. CSS selector (structural match)
    if (evt.selector) {
      try {
        var el = document.querySelector(evt.selector);
        if (el) return el;
      } catch(e) {}
    }

    // 6. Coordinate-based (last resort)
    if (evt.xPct != null && evt.yPct != null) {
      return document.elementFromPoint(
        evt.xPct * window.innerWidth,
        evt.yPct * window.innerHeight
      );
    }

    return null;
  }

  // ── Replay Engine ──
  window.__velReplayRecording = async function(events) {
    var send = window.__velSend;
    var lastTime = 0;

    for (var i = 0; i < events.length; i++) {
      var evt = events[i];

      // Wait for timing (capped at 2s per step)
      var delay = evt.timeMs - lastTime;
      if (delay > 0) {
        await new Promise(function(r) { setTimeout(r, Math.min(delay, 2000)); });
      }
      lastTime = evt.timeMs;

      var el = findElement(evt);

      if (evt.type === 'click') {
        if (el) {
          el.click();
          if (send) send({ type: 'replay_step', status: 'ok', event: evt });
        } else {
          if (send) send({ type: 'replay_step', status: 'not_found', event: evt });
        }
      } else if (evt.type === 'input') {
        if (el) {
          el.value = evt.value;
          el.dispatchEvent(new Event('input', { bubbles: true }));
          el.dispatchEvent(new Event('change', { bubbles: true }));
          if (send) send({ type: 'replay_step', status: 'ok', event: evt });
        } else {
          if (send) send({ type: 'replay_step', status: 'not_found', event: evt });
        }
      } else if (evt.type === 'scroll') {
        window.scrollTo(evt.x, evt.y);
      }
    }
  };
})();
