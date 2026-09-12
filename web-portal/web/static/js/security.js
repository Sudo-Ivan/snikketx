/* Drives the second-factor UI: passkey registration on the security page,
   passkey assertion on the sign-in verification step, and the TOTP QR code.
   Loaded only by the pages that need it, after the vendored qrcode library. */
(function () {
	"use strict";

	function renderQR() {
		if (typeof QRCode === "undefined") {
			return;
		}
		var targets = document.querySelectorAll("[data-qrdata]");
		for (var i = 0; i < targets.length; i++) {
			var target = targets[i];
			if (target.getAttribute("data-qr-rendered") === "1") {
				continue;
			}
			target.setAttribute("data-qr-rendered", "1");
			new QRCode(target, {
				text: target.dataset.qrdata,
				width: 200,
				height: 200,
				correctLevel: QRCode.CorrectLevel.M
			});
		}
	}

	function b64ToBuf(b64) {
		var s = String(b64).replace(/-/g, "+").replace(/_/g, "/");
		while (s.length % 4) {
			s += "=";
		}
		var bin = atob(s);
		var buf = new Uint8Array(bin.length);
		for (var i = 0; i < bin.length; i++) {
			buf[i] = bin.charCodeAt(i);
		}
		return buf.buffer;
	}

	function bufToB64(buf) {
		var bin = "";
		var bytes = new Uint8Array(buf);
		for (var i = 0; i < bytes.byteLength; i++) {
			bin += String.fromCharCode(bytes[i]);
		}
		return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
	}

	function report(btn, msg) {
		var card = btn.closest(".card") || document;
		var el = card.querySelector("[data-passkey-status]");
		if (el) {
			el.textContent = msg;
		}
	}

	function postJSON(url, body, csrf) {
		var headers = { "Accept": "application/json", "X-CSRF-Token": csrf };
		if (body !== null) {
			headers["Content-Type"] = "application/json";
		}
		return fetch(url, {
			method: "POST",
			headers: headers,
			body: body === null ? null : JSON.stringify(body),
			credentials: "same-origin"
		}).then(function (resp) {
			return resp.json().catch(function () { return {}; }).then(function (data) {
				if (!resp.ok) {
					throw new Error(data.error || "Request failed (" + resp.status + ")");
				}
				return data;
			});
		});
	}

	function decodeDescriptor(desc) {
		var out = { id: b64ToBuf(desc.id), type: desc.type };
		if (desc.transports) {
			out.transports = desc.transports;
		}
		return out;
	}

	function creationToPublicKey(creation) {
		var pk = creation.publicKey;
		pk.challenge = b64ToBuf(pk.challenge);
		pk.user.id = b64ToBuf(pk.user.id);
		if (pk.excludeCredentials) {
			pk.excludeCredentials = pk.excludeCredentials.map(decodeDescriptor);
		}
		return pk;
	}

	function requestToPublicKey(assertion) {
		var pk = assertion.publicKey;
		pk.challenge = b64ToBuf(pk.challenge);
		if (pk.allowCredentials) {
			pk.allowCredentials = pk.allowCredentials.map(decodeDescriptor);
		}
		return pk;
	}

	function credentialToJSON(cred) {
		var out = {
			id: cred.id,
			rawId: bufToB64(cred.rawId),
			type: cred.type,
			response: {},
			clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {}
		};
		if (cred.authenticatorAttachment) {
			out.authenticatorAttachment = cred.authenticatorAttachment;
		}
		var resp = cred.response;
		out.response.clientDataJSON = bufToB64(resp.clientDataJSON);
		if (resp.attestationObject) {
			out.response.attestationObject = bufToB64(resp.attestationObject);
			if (resp.getTransports) {
				out.response.transports = resp.getTransports();
			}
		}
		if (resp.authenticatorData) {
			out.response.authenticatorData = bufToB64(resp.authenticatorData);
		}
		if (resp.signature) {
			out.response.signature = bufToB64(resp.signature);
		}
		if (resp.userHandle) {
			out.response.userHandle = bufToB64(resp.userHandle);
		}
		return out;
	}

	function supported() {
		return typeof window.PublicKeyCredential !== "undefined" &&
			typeof navigator.credentials !== "undefined";
	}

	function bindRegister(btn) {
		btn.addEventListener("click", function () {
			if (!supported()) {
				report(btn, "This browser does not support passkeys.");
				return;
			}
			var csrf = btn.dataset.csrf || "";
			var nameInput = document.querySelector("[data-passkey-name]");
			var name = nameInput ? nameInput.value.trim() : "";
			btn.disabled = true;
			report(btn, "Follow the prompt of your browser or device...");
			postJSON("/user/security/passkey/begin", null, csrf)
				.then(function (creation) {
					return navigator.credentials.create({ publicKey: creationToPublicKey(creation) });
				})
				.then(function (cred) {
					var url = "/user/security/passkey/finish";
					if (name !== "") {
						url += "?name=" + encodeURIComponent(name);
					}
					return postJSON(url, credentialToJSON(cred), csrf);
				})
				.then(function () {
					window.location.reload();
				})
				.catch(function (err) {
					report(btn, err.message || "Passkey registration failed.");
				})
				.finally(function () {
					btn.disabled = false;
				});
		});
	}

	function bindLogin(btn) {
		btn.addEventListener("click", function () {
			if (!supported()) {
				report(btn, "This browser does not support passkeys.");
				return;
			}
			var csrf = btn.dataset.csrf || "";
			btn.disabled = true;
			report(btn, "Follow the prompt of your browser or device...");
			postJSON("/login/verify/passkey/begin", null, csrf)
				.then(function (assertion) {
					return navigator.credentials.get({ publicKey: requestToPublicKey(assertion) });
				})
				.then(function (cred) {
					return postJSON("/login/verify/passkey/finish", credentialToJSON(cred), csrf);
				})
				.then(function (data) {
					window.location.assign(data.redirect || "/user/");
				})
				.catch(function (err) {
					report(btn, err.message || "Passkey verification failed.");
				})
				.finally(function () {
					btn.disabled = false;
				});
		});
	}

	function init() {
		renderQR();
		var register = document.querySelectorAll("[data-passkey-register]");
		for (var i = 0; i < register.length; i++) {
			bindRegister(register[i]);
		}
		var login = document.querySelectorAll("[data-passkey-login]");
		for (var j = 0; j < login.length; j++) {
			bindLogin(login[j]);
		}
	}

	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", init);
	} else {
		init();
	}
})();
