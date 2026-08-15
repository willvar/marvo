package cn.willvar.marvo

import org.json.JSONObject

internal object MarvoBridgeScripts {
    fun client(origin: String): String =
        """
        (() => {
          'use strict';
          if (location.origin !== ${JSONObject.quote(origin)}) return;
          const nativeBridge = window.__marvoNative;
          if (!nativeBridge || typeof nativeBridge.postMessage !== 'function') return;
          const pending = new Map();
          nativeBridge.onmessage = event => {
            const raw = event && event.data;
            if (typeof raw !== 'string') return;
            try {
              const envelope = JSON.parse(raw);
              const request = pending.get(envelope.id);
              if (!request) return;
              pending.delete(envelope.id);
              request.resolve(raw);
            } catch (_) {}
          };
          const transport = Object.freeze({
            send(raw) {
              return new Promise((resolve, reject) => {
                let id;
                try { id = JSON.parse(raw).id; } catch (error) { reject(error); return; }
                if (typeof id !== 'string' || !id) {
                  reject({ code: 'INVALID_ARGUMENT', message: 'Bridge request is missing an id' });
                  return;
                }
                pending.set(id, { resolve, reject });
                try { nativeBridge.postMessage(raw); }
                catch (error) { pending.delete(id); reject(error); }
              });
            }
          });
          Object.defineProperty(window, '__marvoTransport', { configurable: true, value: transport });
          let sequence = 0;
          const call = (method, payload) => {
            if (typeof method !== 'string' || !method) {
              return Promise.reject({ code: 'INVALID_ARGUMENT', message: 'method must be a non-empty string' });
            }
            const id = Date.now().toString(36) + '-' + (++sequence).toString(36);
            return transport.send(JSON.stringify({ id, method, payload: payload === undefined ? null : payload }))
              .then(raw => JSON.parse(raw))
              .then(response => {
                if (!response || typeof response.ok !== 'boolean') {
                  throw { code: 'BRIDGE_UNAVAILABLE', message: 'Invalid native response' };
                }
                if (response.ok) return response.result;
                throw response.error || { code: 'IO_ERROR', message: 'Native operation failed' };
              });
          };
          const bridge = Object.freeze({
            ready: () => Promise.resolve(),
            call,
            back: () => {
              try {
                return typeof window.__marvoHandleBack === 'function' && window.__marvoHandleBack() === true;
              } catch (_) { return false; }
            }
          });
          Object.defineProperty(window, 'marvo', {
            configurable: true,
            enumerable: true,
            value: bridge
          });
        })();
        """.trimIndent()
}
