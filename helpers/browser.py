"""
VelBridge Browser Helper — CDP client for AI agents.
Controls browsers via the VelBridge relay CDP proxy.
Single-file, sync API. Requires: websockets (pip install websockets)

Usage:
    from browser import Browser

    with Browser(relay_token="<token>", server="https://<domain>") as b:
        tab = b.new_tab("https://www.google.com")  # Always open a NEW tab
        b.type_text("hello world", "textarea[name=q]")
        b.press_key("Enter")
        b.wait_for_load()
        print(b.read_page())

⚠️  NEVER navigate existing tabs blindly — one of them is the bridge tab.
    Always use new_tab() or filter targets (skip URLs containing /bridge/debug/bridge).
"""

import asyncio
import base64
import json
import math
import os
import random
import threading
import time
from typing import Any, Dict, List, Optional, Union

import websockets
import websockets.sync.client as ws_sync


class BrowserError(Exception):
    """Base error for browser operations."""
    pass


class ConnectionError(BrowserError):
    """WebSocket connection failed or lost."""
    pass


class TimeoutError(BrowserError):
    """Operation timed out."""
    pass


class CDPError(BrowserError):
    """CDP protocol returned an error."""
    def __init__(self, message, code=None, data=None):
        super().__init__(message)
        self.code = code
        self.data = data


# ---------------------------------------------------------------------------
# Mouse math helpers
# ---------------------------------------------------------------------------

def _bezier_point(t: float, p0: float, p1: float, p2: float, p3: float) -> float:
    """Cubic Bezier curve point at parameter t."""
    return (1 - t) ** 3 * p0 + 3 * (1 - t) ** 2 * t * p1 + 3 * (1 - t) * t ** 2 * p2 + t ** 3 * p3


def _ease_in_out(t: float) -> float:
    """Ease-in-out timing function."""
    if t < 0.5:
        return 2 * t * t
    return -1 + (4 - 2 * t) * t


def _generate_bezier_path(x0: float, y0: float, x1: float, y1: float, num_points: int = 0) -> list:
    """Generate a natural mouse path from (x0,y0) to (x1,y1) using cubic Bezier curves."""
    dist = math.hypot(x1 - x0, y1 - y0)
    if num_points <= 0:
        num_points = max(20, min(50, int(dist / 10)))

    # Perpendicular direction for control point offsets
    dx, dy = x1 - x0, y1 - y0
    length = max(dist, 1)
    perp_x, perp_y = -dy / length, dx / length

    # Random control points offset from the line
    offset1 = random.uniform(30, 100) * random.choice([-1, 1])
    offset2 = random.uniform(30, 100) * random.choice([-1, 1])

    cp1_x = x0 + dx * 0.33 + perp_x * offset1
    cp1_y = y0 + dy * 0.33 + perp_y * offset1
    cp2_x = x0 + dx * 0.66 + perp_x * offset2
    cp2_y = y0 + dy * 0.66 + perp_y * offset2

    points = []
    for i in range(num_points + 1):
        raw_t = i / num_points
        t = _ease_in_out(raw_t)
        bx = _bezier_point(t, x0, cp1_x, cp2_x, x1)
        by = _bezier_point(t, y0, cp1_y, cp2_y, y1)
        # Micro-jitter (skip endpoints)
        if 0 < i < num_points:
            bx += random.uniform(-3, 3)
            by += random.uniform(-3, 3)
        points.append((bx, by))
    return points


# ---------------------------------------------------------------------------
# Key code mappings
# ---------------------------------------------------------------------------

_KEY_MAP = {
    "Enter": {"key": "Enter", "code": "Enter", "keyCode": 13, "which": 13, "text": "\r"},
    "Tab": {"key": "Tab", "code": "Tab", "keyCode": 9, "which": 9},
    "Escape": {"key": "Escape", "code": "Escape", "keyCode": 27, "which": 27},
    "Backspace": {"key": "Backspace", "code": "Backspace", "keyCode": 8, "which": 8},
    "Delete": {"key": "Delete", "code": "Delete", "keyCode": 46, "which": 46},
    "ArrowUp": {"key": "ArrowUp", "code": "ArrowUp", "keyCode": 38, "which": 38},
    "ArrowDown": {"key": "ArrowDown", "code": "ArrowDown", "keyCode": 40, "which": 40},
    "ArrowLeft": {"key": "ArrowLeft", "code": "ArrowLeft", "keyCode": 37, "which": 37},
    "ArrowRight": {"key": "ArrowRight", "code": "ArrowRight", "keyCode": 39, "which": 39},
    "Space": {"key": " ", "code": "Space", "keyCode": 32, "which": 32, "text": " "},
}


# ---------------------------------------------------------------------------
# Browser class
# ---------------------------------------------------------------------------

class Browser:
    """
    Synchronous CDP browser controller via OpenClaw relay.

    Usage:
        b = Browser(relay_token="YOUR_TOKEN", server="http://localhost:3700")
        b.connect()
        b.navigate("https://example.com")
        print(b.read_page())
        b.disconnect()
    """

    def __init__(
        self,
        relay_token: str,
        server: str = "http://localhost:3700",
        human_mode: bool = False,
        timeout: float = 10.0,
    ):
        self.relay_token = relay_token
        self.server = server.rstrip("/")
        self.human_mode = human_mode
        self.timeout = timeout

        self._ws = None
        self._msg_id = 0
        self._lock = threading.Lock()
        self._session_id: Optional[str] = None  # flattened session for current target
        self._current_target: Optional[str] = None
        self._mouse_x: float = 0
        self._mouse_y: float = 0
        self._pending: Dict[int, Any] = {}
        self._events: List[dict] = []

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _ws_url(self) -> str:
        base = self.server.replace("http://", "ws://").replace("https://", "wss://")
        return f"{base}/bridge/debug/cdp/ws?token={self.relay_token}"

    def _next_id(self) -> int:
        self._msg_id += 1
        return self._msg_id

    def _send(self, method: str, params: dict = None, session_id: str = None, timeout: float = None) -> dict:
        """Send a CDP command and wait for its response."""
        if self._ws is None:
            raise ConnectionError("Not connected. Call connect() first.")
        timeout = timeout or self.timeout
        msg_id = self._next_id()
        msg: Dict[str, Any] = {"id": msg_id, "method": method}
        if params:
            msg["params"] = params
        if session_id:
            msg["sessionId"] = session_id

        with self._lock:
            self._ws.send(json.dumps(msg))

        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                break
            try:
                self._ws.socket.settimeout(min(remaining, 0.5))
                raw = self._ws.recv()
                data = json.loads(raw)
                if data.get("id") == msg_id:
                    if "error" in data:
                        e = data["error"]
                        raise CDPError(e.get("message", str(e)), e.get("code"), e.get("data"))
                    return data.get("result", {})
                # Store events / other responses
                self._events.append(data)
            except TimeoutError:
                continue
            except websockets.exceptions.ConnectionClosed:
                self._ws = None
                raise ConnectionError("WebSocket connection closed unexpectedly.")
        raise TimeoutError(f"Timeout waiting for response to {method} (id={msg_id})")

    def _send_to_target(self, method: str, params: dict = None, timeout: float = None) -> dict:
        """Send a command to the current attached target."""
        if not self._session_id:
            raise BrowserError("No target attached. Call navigate() or new_tab() first.")
        return self._send(method, params, session_id=self._session_id, timeout=timeout)

    def _drain_events(self, timeout: float = 0.1):
        """Read pending events without blocking long."""
        if self._ws is None:
            return
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            try:
                self._ws.socket.settimeout(0.05)
                raw = self._ws.recv()
                self._events.append(json.loads(raw))
            except Exception:
                break

    def _wait_event(self, method: str, timeout: float = None) -> dict:
        """Wait for a specific CDP event."""
        timeout = timeout or self.timeout
        deadline = time.monotonic() + timeout
        # Check buffered events first
        for i, ev in enumerate(self._events):
            if ev.get("method") == method:
                self._events.pop(i)
                return ev
        while time.monotonic() < deadline:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                break
            try:
                self._ws.socket.settimeout(min(remaining, 0.5))
                raw = self._ws.recv()
                data = json.loads(raw)
                if data.get("method") == method:
                    return data
                self._events.append(data)
            except Exception:
                continue
        raise TimeoutError(f"Timeout waiting for event {method}")

    def _human_pause(self, lo: float = 0.1, hi: float = 0.5):
        """Random pause when human_mode is enabled."""
        if self.human_mode:
            time.sleep(random.uniform(lo, hi))

    def _attach_target(self, target_id: str):
        """Attach to a target with flattened session."""
        result = self._send("Target.attachToTarget", {"targetId": target_id, "flatten": True})
        self._session_id = result.get("sessionId")
        self._current_target = target_id
        if not self._session_id:
            # Try to find sessionId from events
            self._drain_events(0.5)
            for ev in self._events:
                if ev.get("method") == "Target.attachedToTarget":
                    p = ev.get("params", {})
                    if p.get("targetInfo", {}).get("targetId") == target_id:
                        self._session_id = p.get("sessionId")
                        break
        if not self._session_id:
            raise BrowserError(f"Failed to get sessionId for target {target_id}")

    # ------------------------------------------------------------------
    # Connection
    # ------------------------------------------------------------------

    def connect(self):
        """Connect to the relay CDP proxy WebSocket."""
        try:
            self._ws = ws_sync.connect(self._ws_url(), open_timeout=self.timeout)
        except Exception as e:
            raise ConnectionError(f"Failed to connect to relay at {self.server}: {e}")
        return self

    def disconnect(self):
        """Close the WebSocket connection."""
        if self._ws:
            try:
                self._ws.close()
            except Exception:
                pass
            self._ws = None

    def reconnect(self):
        """Reconnect to the relay."""
        self.disconnect()
        self.connect()
        return self

    # ------------------------------------------------------------------
    # Tabs
    # ------------------------------------------------------------------

    def list_tabs(self) -> List[Dict[str, str]]:
        """Return list of open tabs: [{id, title, url}, ...]"""
        result = self._send("Target.getTargets")
        tabs = []
        for info in result.get("targetInfos", []):
            if info.get("type") == "page":
                tabs.append({
                    "id": info["targetId"],
                    "title": info.get("title", ""),
                    "url": info.get("url", ""),
                })
        return tabs

    def new_tab(self, url: str = "about:blank") -> str:
        """Create a new tab, navigate to url, attach to it. Returns target_id."""
        result = self._send("Target.createTarget", {"url": url})
        target_id = result["targetId"]
        self._attach_target(target_id)
        if self.human_mode and url != "about:blank":
            self.simulate_human_presence(random.uniform(1, 3))
        return target_id

    def close_tab(self, tab_id: str):
        """Close a tab by target id."""
        self._send("Target.closeTarget", {"targetId": tab_id})
        if tab_id == self._current_target:
            self._session_id = None
            self._current_target = None

    # ------------------------------------------------------------------
    # Navigation
    # ------------------------------------------------------------------

    def navigate(self, url: str, tab_id: str = None):
        """Navigate current (or specified) tab to url."""
        if tab_id and tab_id != self._current_target:
            self._attach_target(tab_id)
        if not self._session_id:
            # Auto-attach to first available tab
            tabs = self.list_tabs()
            if not tabs:
                self.new_tab(url)
                return
            self._attach_target(tabs[0]["id"])
        self._send_to_target("Page.enable")
        self._send_to_target("Page.navigate", {"url": url})
        self.wait_for_load()
        if self.human_mode:
            self.simulate_human_presence(random.uniform(1, 3))

    def wait_for_load(self, timeout: float = 10):
        """Wait for page load complete."""
        try:
            self._wait_event("Page.loadEventFired", timeout=timeout)
        except TimeoutError:
            pass  # Best effort — page may already be loaded

    # ------------------------------------------------------------------
    # Reading
    # ------------------------------------------------------------------

    def evaluate(self, expression: str, timeout: float = None) -> Any:
        """Execute JS expression and return the result value."""
        result = self._send_to_target(
            "Runtime.evaluate",
            {"expression": expression, "returnByValue": True, "awaitPromise": True},
            timeout=timeout,
        )
        exc = result.get("exceptionDetails")
        if exc:
            text = exc.get("text", "") or exc.get("exception", {}).get("description", "JS error")
            raise CDPError(f"JS error: {text}")
        return result.get("result", {}).get("value")

    def read_page(self) -> str:
        """Return the visible text content of the page."""
        return self.evaluate("document.body.innerText") or ""

    def read_html(self) -> str:
        """Return the full HTML of the page."""
        return self.evaluate("document.documentElement.outerHTML") or ""

    def get_title(self) -> str:
        """Return document.title."""
        return self.evaluate("document.title") or ""

    def get_url(self) -> str:
        """Return the current page URL."""
        return self.evaluate("window.location.href") or ""

    # ------------------------------------------------------------------
    # Mouse simulation
    # ------------------------------------------------------------------

    def human_mouse_move(self, from_x: float, from_y: float, to_x: float, to_y: float):
        """Move mouse naturally from (from_x, from_y) to (to_x, to_y) using Bezier curves."""
        points = _generate_bezier_path(from_x, from_y, to_x, to_y)
        for px, py in points:
            self._send_to_target("Input.dispatchMouseEvent", {
                "type": "mouseMoved",
                "x": round(px, 2),
                "y": round(py, 2),
            })
            time.sleep(random.uniform(0.005, 0.03))
        self._mouse_x = to_x
        self._mouse_y = to_y

    def random_mouse_jitter(self):
        """Small random mouse movements to simulate idle human."""
        for _ in range(random.randint(2, 5)):
            dx = random.uniform(-15, 15)
            dy = random.uniform(-15, 15)
            nx = max(0, self._mouse_x + dx)
            ny = max(0, self._mouse_y + dy)
            self._send_to_target("Input.dispatchMouseEvent", {
                "type": "mouseMoved",
                "x": round(nx, 2),
                "y": round(ny, 2),
            })
            self._mouse_x, self._mouse_y = nx, ny
            time.sleep(random.uniform(0.05, 0.2))

    def simulate_human_presence(self, duration_seconds: float = 2):
        """Random scrolls, mouse movements, and pauses to look human."""
        end = time.monotonic() + duration_seconds
        while time.monotonic() < end:
            action = random.choice(["jitter", "scroll", "pause"])
            if action == "jitter":
                self.random_mouse_jitter()
            elif action == "scroll":
                self.scroll(random.choice(["up", "down"]), random.randint(50, 200))
            else:
                time.sleep(random.uniform(0.3, 0.8))

    # ------------------------------------------------------------------
    # Interaction
    # ------------------------------------------------------------------

    def click(self, x: float, y: float):
        """Click at (x, y) with natural mouse movement."""
        # Add small random offset
        tx = x + random.uniform(-3, 3)
        ty = y + random.uniform(-3, 3)

        if self.human_mode:
            self.random_mouse_jitter()

        self.human_mouse_move(self._mouse_x, self._mouse_y, tx, ty)

        for event_type in ["mousePressed", "mouseReleased"]:
            self._send_to_target("Input.dispatchMouseEvent", {
                "type": event_type,
                "x": round(tx, 2),
                "y": round(ty, 2),
                "button": "left",
                "clickCount": 1,
            })
            time.sleep(random.uniform(0.03, 0.1))

        self._human_pause()

    def click_element(self, selector: str):
        """Find element by CSS selector, get its center, click with natural movement."""
        js = f"""
        (() => {{
            const el = document.querySelector({json.dumps(selector)});
            if (!el) return null;
            const r = el.getBoundingClientRect();
            return {{x: r.x + r.width/2, y: r.y + r.height/2}};
        }})()
        """
        pos = self.evaluate(js)
        if not pos:
            raise BrowserError(f"Element not found: {selector}")
        self.click(pos["x"], pos["y"])

    def type_text(self, text: str, selector: str = None):
        """Type text character by character with random delays. Optionally click element first."""
        if selector:
            self.click_element(selector)
            time.sleep(random.uniform(0.1, 0.3))

        if self.human_mode:
            time.sleep(random.uniform(0.2, 0.5))

        for ch in text:
            self._send_to_target("Input.dispatchKeyEvent", {
                "type": "keyDown",
                "text": ch,
                "key": ch,
                "unmodifiedText": ch,
            })
            self._send_to_target("Input.dispatchKeyEvent", {
                "type": "keyUp",
                "key": ch,
            })
            time.sleep(random.uniform(0.05, 0.15))

        self._human_pause()

    def press_key(self, key: str):
        """Press a keyboard key (Enter, Tab, Escape, etc.)."""
        info = _KEY_MAP.get(key, {"key": key, "code": key})
        down_params = {"type": "keyDown", **info}
        up_params = {"type": "keyUp", "key": info["key"], "code": info.get("code", key)}
        self._send_to_target("Input.dispatchKeyEvent", down_params)
        time.sleep(random.uniform(0.03, 0.08))
        self._send_to_target("Input.dispatchKeyEvent", up_params)
        self._human_pause()

    def scroll(self, direction: str = "down", amount: int = 300):
        """Scroll the page. direction: up/down/left/right."""
        dx, dy = 0, 0
        if direction == "down":
            dy = amount
        elif direction == "up":
            dy = -amount
        elif direction == "right":
            dx = amount
        elif direction == "left":
            dx = -amount

        self._send_to_target("Input.dispatchMouseEvent", {
            "type": "mouseWheel",
            "x": round(self._mouse_x),
            "y": round(self._mouse_y),
            "deltaX": dx,
            "deltaY": dy,
        })
        self._human_pause(0.1, 0.3)

    # ------------------------------------------------------------------
    # Screenshots
    # ------------------------------------------------------------------

    def screenshot(self, path: str = None) -> str:
        """Capture full page screenshot. Returns base64 data. Saves to path if given."""
        result = self._send_to_target("Page.captureScreenshot", {"format": "png"})
        data = result["data"]
        if path:
            with open(path, "wb") as f:
                f.write(base64.b64decode(data))
        return data

    def screenshot_element(self, selector: str, path: str = None) -> str:
        """Screenshot a specific element by CSS selector."""
        js = f"""
        (() => {{
            const el = document.querySelector({json.dumps(selector)});
            if (!el) return null;
            const r = el.getBoundingClientRect();
            return {{x: r.x, y: r.y, width: r.width, height: r.height}};
        }})()
        """
        rect = self.evaluate(js)
        if not rect:
            raise BrowserError(f"Element not found: {selector}")
        result = self._send_to_target("Page.captureScreenshot", {
            "format": "png",
            "clip": {
                "x": rect["x"],
                "y": rect["y"],
                "width": rect["width"],
                "height": rect["height"],
                "scale": 1,
            },
        })
        data = result["data"]
        if path:
            with open(path, "wb") as f:
                f.write(base64.b64decode(data))
        return data

    # ------------------------------------------------------------------
    # Context manager
    # ------------------------------------------------------------------

    def __enter__(self):
        self.connect()
        return self

    def __exit__(self, *args):
        self.disconnect()
