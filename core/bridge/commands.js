/**
 * VelBridge Proxy Mode — Command Handler
 * Handles commands from the agent: screenshot, navigate, scroll, click, eval, info.
 * Screenshot uses getDisplayMedia (primary) with html2canvas fallback.
 */
(function() {
  'use strict';

  var displayStream = null; // Reusable getDisplayMedia stream

  /**
   * Capture a screenshot using getDisplayMedia (primary) or html2canvas (fallback).
   * Returns { data: dataURL, method: 'native'|'html2canvas' }
   */
  async function captureScreenshot(fullPage) {
    // Primary: getDisplayMedia
    try {
      if (!displayStream || !displayStream.active) {
        displayStream = await navigator.mediaDevices.getDisplayMedia({
          video: { preferCurrentTab: true },
          preferCurrentTab: true
        });
        // Clean up when user stops sharing
        displayStream.getVideoTracks()[0].addEventListener('ended', function() {
          displayStream = null;
        });
      }

      var track = displayStream.getVideoTracks()[0];
      var settings = track.getSettings();
      var surface = settings.displaySurface || 'unknown';

      // Use ImageCapture API if available
      if (typeof ImageCapture !== 'undefined') {
        var capture = new ImageCapture(track);
        var bitmap = await capture.grabFrame();
        var canvas = document.createElement('canvas');
        canvas.width = bitmap.width;
        canvas.height = bitmap.height;
        var ctx = canvas.getContext('2d');
        ctx.drawImage(bitmap, 0, 0);
        return { data: canvas.toDataURL('image/png'), method: 'native', surface: surface };
      }

      // Fallback: video element capture
      var video = document.createElement('video');
      video.srcObject = displayStream;
      video.muted = true;
      await video.play();
      // Wait a frame
      await new Promise(function(r) { requestAnimationFrame(r); });
      var canvas = document.createElement('canvas');
      canvas.width = video.videoWidth;
      canvas.height = video.videoHeight;
      var ctx = canvas.getContext('2d');
      ctx.drawImage(video, 0, 0);
      video.pause();
      video.srcObject = null;
      return { data: canvas.toDataURL('image/png'), method: 'native', surface: surface };

    } catch(e) {
      // getDisplayMedia denied or unavailable — fall through to html2canvas
    }

    // Viewport capture (default for non-fullPage) — fast and reliable
    if (!fullPage) {
      try {
        var vCanvas = document.createElement('canvas');
        var dpr = window.devicePixelRatio || 1;
        vCanvas.width = window.innerWidth * dpr;
        vCanvas.height = window.innerHeight * dpr;
        var vCtx = vCanvas.getContext('2d');
        vCtx.scale(dpr, dpr);

        // Use html2canvas but constrained to viewport dimensions only (much faster)
        if (typeof html2canvas === 'function') {
          var vpCanvas = await Promise.race([
            html2canvas(document.body, {
              useCORS: true,
              scale: dpr,
              width: window.innerWidth,
              height: window.innerHeight,
              x: window.scrollX,
              y: window.scrollY,
              windowWidth: window.innerWidth,
              windowHeight: window.innerHeight
            }),
            new Promise(function(_, reject) {
              setTimeout(function() { reject(new Error('viewport timeout')); }, 5000);
            })
          ]);
          return { data: vpCanvas.toDataURL('image/png'), method: 'viewport' };
        }
      } catch(e) {
        // Viewport capture failed, continue to full html2canvas
      }
    }

    // Full-page html2canvas (opt-in via fullPage: true, or fallback)
    if (typeof html2canvas === 'function') {
      try {
        var canvas = await Promise.race([
          html2canvas(document.documentElement, {
            useCORS: true,
            scale: window.devicePixelRatio || 1,
            width: document.documentElement.clientWidth,
            height: document.documentElement.scrollHeight
          }),
          new Promise(function(_, reject) {
            setTimeout(function() { reject(new Error('html2canvas timeout')); }, 10000);
          })
        ]);
        return { data: canvas.toDataURL('image/png'), method: 'html2canvas' };
      } catch(e) {
        return { data: null, method: 'error', error: e.message };
      }
    }

    return { data: null, method: 'error', error: 'No screenshot method available' };
  }

  /**
   * Handle a command from the agent (received via WebSocket).
   */
  window.__velHandleCommand = async function(cmd) {
    var send = window.__velSend;
    if (!send) return;

    switch (cmd.type) {
      case 'screenshot': {
        var result = await captureScreenshot(cmd.fullPage);
        send({ type: 'screenshot', data: result.data, method: result.method, surface: result.surface });
        break;
      }

      case 'navigate':
        window.location.href = cmd.url;
        break;

      case 'scroll':
        window.scrollTo(cmd.x || 0, cmd.y || 0);
        send({ type: 'scrolled', x: window.scrollX, y: window.scrollY });
        break;

      case 'click':
        var el = document.elementFromPoint(cmd.x, cmd.y);
        if (el) el.click();
        break;

      case 'eval':
        try {
          var result = new Function(cmd.js)();
          send({ type: 'eval_result', result: String(result) });
        } catch(e) {
          send({ type: 'eval_result', error: e.message });
        }
        break;

      case 'info':
        send({
          type: 'info',
          title: document.title,
          url: window.location.href,
          viewport: { w: window.innerWidth, h: window.innerHeight },
          scroll: { x: window.scrollX, y: window.scrollY },
          dpr: window.devicePixelRatio
        });
        break;

      case 'start_recording':
        if (window.__velStartRecording) window.__velStartRecording();
        break;

      case 'stop_recording':
        if (window.__velStopRecording) {
          var recording = window.__velStopRecording();
          send({ type: 'recording', data: recording });
        }
        break;

      case 'replay':
        if (window.__velReplayRecording) {
          await window.__velReplayRecording(cmd.data);
          send({ type: 'replay_done' });
        }
        break;

      case 'ping':
        send({ type: 'pong' });
        break;
    }
  };

  // Expose screenshot function for other scripts
  window.__velCaptureScreenshot = captureScreenshot;
})();
