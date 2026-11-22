// Runs in the page's main world at document_start. We deliberately keep the
// browser's genuine Linux platform and Chrome user agent so the UA, the
// navigator.platform and the server-side Sec-CH-UA-Platform header stay
// mutually consistent; spoofing just the platform would create a mismatch that
// fingerprints as automated. What we do remove are the tells a GPU-less,
// CDP-driven container adds: the automation flag and SwiftShader WebGL.
(() => {
  "use strict";

  const originalToString = Function.prototype.toString;
  const fakeNative = new WeakMap();

  // Keep patched functions looking native to Function.prototype.toString, which
  // is itself a common tamper check.
  function markNative(fn, name) {
    fakeNative.set(fn, "function " + name + "() { [native code] }");
    return fn;
  }

  Function.prototype.toString = markNative(function toString() {
    const faked = fakeNative.get(this);
    return faked !== undefined ? faked : originalToString.call(this);
  }, "toString");

  function spoofGetter(target, prop, value) {
    try {
      Object.defineProperty(target, prop, {
        get: markNative(function () {
          return value;
        }, "get " + prop),
        configurable: true,
      });
    } catch (e) {
      // Property not configurable; leave the original.
    }
  }

  // 1. A real desktop browser is never "webdriver". This also covers the case
  //    where the process was started with --enable-automation.
  spoofGetter(Navigator.prototype, "webdriver", false);

  // 2. WebGL. The only renderer in this container is SwiftShader, whose
  //    UNMASKED_RENDERER string ("...SwiftShader Device... SwiftShader driver")
  //    is an unambiguous automation/headless signal. Report a common Intel GPU
  //    instead. Values are only substituted while WEBGL_debug_renderer_info is
  //    enabled, matching Chrome's own behaviour.
  const UNMASKED_VENDOR_WEBGL = 0x9245;
  const UNMASKED_RENDERER_WEBGL = 0x9246;
  const SPOOFED = {};
  SPOOFED[UNMASKED_VENDOR_WEBGL] = "Google Inc. (Intel)";
  SPOOFED[UNMASKED_RENDERER_WEBGL] =
    "ANGLE (Intel, Mesa Intel(R) UHD Graphics 630 (CFL GT2), OpenGL 4.6 (Core Profile) Mesa 23.0.4)";

  const debugEnabled = new WeakSet();

  // WebGLRenderingContext and WebGL2RenderingContext can share (or inherit) the
  // same method objects. Patch each method once, capturing the native function
  // before it is replaced, so a shared method is never wrapped twice.
  function patchMethod(proto, name, wrap) {
    if (!proto) return;
    const original = proto[name];
    if (typeof original !== "function" || fakeNative.has(original)) return;
    try {
      Object.defineProperty(proto, name, {
        value: markNative(wrap(original), name),
        configurable: true,
        writable: true,
      });
    } catch (e) {
      // Context not patchable; leave it.
    }
  }

  function patchWebGL(proto) {
    patchMethod(proto, "getExtension", (original) =>
      function getExtension(name) {
        const ext = original.apply(this, arguments);
        if (ext && name === "WEBGL_debug_renderer_info") debugEnabled.add(this);
        return ext;
      }
    );
    patchMethod(proto, "getParameter", (original) =>
      function getParameter(pname) {
        if (debugEnabled.has(this) && SPOOFED[pname] !== undefined) {
          return SPOOFED[pname];
        }
        return original.apply(this, arguments);
      }
    );
  }

  patchWebGL(window.WebGLRenderingContext && WebGLRenderingContext.prototype);
  patchWebGL(window.WebGL2RenderingContext && WebGL2RenderingContext.prototype);
})();
