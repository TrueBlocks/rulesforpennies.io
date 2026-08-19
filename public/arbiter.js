(function () {
  "use strict";

  var API_BASE = "";
  var ADMIN_KEY = "arbiter_admin";
  var ADMIN_SECRET = "penny1793";

  function checkAdmin() {
    var params = new URLSearchParams(window.location.search);
    if (params.get("admin") === ADMIN_SECRET) {
      localStorage.setItem(ADMIN_KEY, "true");
      window.history.replaceState({}, "", window.location.pathname);
    }
    if (params.get("admin") === "off") {
      localStorage.removeItem(ADMIN_KEY);
      window.history.replaceState({}, "", window.location.pathname);
    }
    return localStorage.getItem(ADMIN_KEY) === "true";
  }

  var isAdmin = checkAdmin();

  function getSessionToken() {
    var token = localStorage.getItem("arbiter_session");
    if (!token) {
      if (crypto && typeof crypto.randomUUID === "function") {
        token = crypto.randomUUID();
      } else {
        token = "sess-" + Date.now() + "-" + Math.random().toString(36).slice(2, 10);
      }
      localStorage.setItem("arbiter_session", token);
    }
    return token;
  }

  var backendAvailable = false;

  function setStatus(connected, message) {
    var el = document.getElementById("arbiter-status");
    if (!el) return;
    backendAvailable = connected;
    el.classList.remove("arbiter-status--checking", "arbiter-status--ok", "arbiter-status--error");
    if (connected) {
      el.classList.add("arbiter-status--ok");
      el.textContent = message || "Arbiter is listening.";
    } else {
      el.classList.add("arbiter-status--error");
      el.textContent = message || "Arbiter is unreachable. Rulings cannot be rendered.";
    }
  }

  function checkBackend() {
    return new Promise(function (resolve) {
      var controller = new AbortController();
      var timer = setTimeout(function () {
        controller.abort();
        console.error("[arbiter] health check timed out");
        resolve(false);
      }, 3000);

      fetch(API_BASE + "/health", { signal: controller.signal })
        .then(function (resp) {
          clearTimeout(timer);
          if (!resp.ok) {
            console.error("[arbiter] health check returned HTTP", resp.status);
          }
          resolve(resp.ok);
        })
        .catch(function (err) {
          clearTimeout(timer);
          console.error("[arbiter] health check failed:", err.message || err);
          resolve(false);
        });
    });
  }

  function initializeApp() {
    setStatus(false, "Checking for the Arbiter…");
    checkBackend().then(function (ok) {
      if (!ok) {
        setStatus(false, "Arbiter is unreachable: arbiterd is not running. Rulings cannot be rendered.");
        var historyList = document.getElementById("history-list");
        if (historyList) {
          historyList.innerHTML = "<li class='arbiter-unavailable'>Prior rulings unavailable: Arbiter is not running.</li>";
        }
        var suggestionsPanel = document.getElementById("suggestions-panel");
        if (suggestionsPanel) {
          suggestionsPanel.style.display = "none";
        }
        return;
      }
      loadRulings(true);
    });
  }

  var deliberationMessages = [
    "The arbiter is reviewing the docket…",
    "Consulting the Official Code…",
    "Weighing precedent and circumstance…",
    "Deliberating with appropriate gravity…",
    "The docket is crowded; taking a short breather…",
    "Preparing the ruling…"
  ];

  function startDeliberationAnimation(loadingEl) {
    var index = 0;
    loadingEl.textContent = deliberationMessages[0];
    return setInterval(function () {
      index = (index + 1) % deliberationMessages.length;
      loadingEl.textContent = deliberationMessages[index];
    }, 2500);
  }

  function stopDeliberationAnimation(intervalId, loadingEl) {
    clearInterval(intervalId);
    loadingEl.textContent = "The arbiter is deliberating…";
  }

  function formatRuling(text, rules) {
    var escaped = text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
    var summaryMap = {};
    if (rules) {
      rules.forEach(function (r) {
        if (r.code) summaryMap[r.code] = r.summary;
        if (r.title) summaryMap[r.title.toLowerCase()] = r.summary;
      });
    }
    var formatted = escaped.replace(/\u00a7(\d+\.\d+)\s+\*\*([^*]+)\*\*/g, function (_, code, name) {
      var summary = (summaryMap["\u00a7" + code] || summaryMap[name.toLowerCase()] || "").replace(/<br>/g, ' ').replace(/\n/g, ' ');
      var tip = summary ? ' data-tooltip="' + summary.replace(/"/g, '&quot;') + '"' : "";
      return '<span class="rule-code">\u00a7' + code + '</span> <span class="rule-name"' + tip + '>' + name + '</span>';
    });
    formatted = formatted.replace(/\*\*([^*]+)\*\*/g, function (_, name) {
      var summary = (summaryMap[name.toLowerCase()] || "").replace(/<br>/g, ' ').replace(/\n/g, ' ');
      var tip = summary ? ' data-tooltip="' + summary.replace(/"/g, '&quot;') + '"' : "";
      return '<span class="rule-name"' + tip + '>' + name + '</span>';
    });
    formatted = formatted.replace(/\n/g, "<br>");
    formatted = formatted.replace(/(https:\/\/[^\s<]+)/g, '<a href="$1" target="_blank" rel="noopener">$1</a>');
    return formatted;
  }

  function truncate(text, max) {
    if (text.length <= max) return text;
    return text.substring(0, max) + "\u2026";
  }

  function loadRulings(autoShow) {
    var historyList = document.getElementById("history-list");
    var suggestionsPanel = document.getElementById("suggestions-panel");
    var suggestionsList = document.getElementById("suggestions-list");

    fetch(API_BASE + "/rulings")
      .then(function (resp) {
        if (!resp.ok) {
          throw new Error("HTTP " + resp.status);
        }
        return resp.json();
      })
      .then(function (items) {
        setStatus(true, "Arbiter is listening.");
        historyList.innerHTML = "";
        suggestionsList.innerHTML = "";

        var priorRulings = [];
        var proposedRules = [];
        items.forEach(function (item) {
          if (item.is_proposed) {
            proposedRules.push(item);
          } else {
            priorRulings.push(item);
          }
        });

        priorRulings.forEach(function (item) {
          var li = document.createElement("li");
          var a = document.createElement("a");
          a.textContent = truncate(item.situation, 40);
          a.title = item.situation;
          a.addEventListener("click", function () {
            showRuling(item);
          });
          li.appendChild(a);
          if (isAdmin) {
            var del = document.createElement("span");
            del.className = "delete-btn";
            del.textContent = "\u00d7";
            del.addEventListener("click", function (e) {
              e.stopPropagation();
              deleteRuling(item.id);
            });
            li.appendChild(del);
          }
          historyList.appendChild(li);
        });

        if (proposedRules.length === 0) {
          suggestionsPanel.style.display = "none";
        } else {
          suggestionsPanel.style.display = "";
          proposedRules.forEach(function (item) {
            var li = document.createElement("li");
            var tip = item.situation + (item.rationale ? " \u2014 Reason: " + item.rationale : "");
            li.setAttribute("data-tooltip", tip);
            var span = document.createElement("span");
            span.className = "suggestion-text";
            span.textContent = truncate(item.situation, 50);
            li.appendChild(span);
            if (isAdmin) {
              var del = document.createElement("span");
              del.className = "delete-btn";
              del.textContent = "\u00d7";
              del.addEventListener("click", function (e) {
                e.stopPropagation();
                deleteRuling(item.id);
              });
              li.appendChild(del);
            }
            suggestionsList.appendChild(li);
          });
        }

        // Check URL hash for deep link (only on initial load)
        if (autoShow) {
          var hash = window.location.hash.substring(1);
          if (hash) {
            var match = items.find(function (r) { return r.slug === hash; });
            if (match) showRuling(match);
          }
        }
      })
      .catch(function (err) {
        console.error("[arbiter] failed to load rulings:", err.message || err);
        setStatus(false, "Arbiter is unreachable: arbiterd is not running. Rulings cannot be rendered.");
        historyList.innerHTML = "<li class='arbiter-unavailable'>Prior rulings unavailable: Arbiter is not running.</li>";
        suggestionsPanel.style.display = "none";
      });
  }

  function showRuling(item) {
    var rulingSection = document.getElementById("ruling-section");
    var rulingText = document.getElementById("ruling-text");
    var textarea = document.getElementById("situation");
    textarea.value = item.situation;
    var len = item.situation.length;
    document.getElementById("char-count").textContent = len + " / 2000";
    document.getElementById("submit-btn").disabled = false;
    rulingText.innerHTML = formatRuling(item.ruling, item.rules);
    rulingSection.classList.add("visible");
    document.getElementById("loading").classList.remove("visible");
    document.getElementById("suggest-btn").style.display = "none";
    window.history.replaceState(null, '', '#' + item.slug);
  }

  function deleteRuling(id) {
    document.getElementById("tooltip-box").style.display = "none";
    var headers = { "X-Admin-Token": ADMIN_SECRET };
    fetch(API_BASE + "/rulings/" + id, { method: "DELETE", headers: headers })
      .then(function () { loadRulings(false); });
  }

  function init() {
    var textarea = document.getElementById("situation");
    var charCount = document.getElementById("char-count");
    var submitBtn = document.getElementById("submit-btn");
    var loading = document.getElementById("loading");
    var errorMsg = document.getElementById("error-msg");
    var rulingSection = document.getElementById("ruling-section");
    var rulingText = document.getElementById("ruling-text");

    initializeApp();

    textarea.addEventListener("input", function () {
      var len = textarea.value.length;
      charCount.textContent = len + " / 2000";
      submitBtn.disabled = len === 0 || len > 2000;
    });

    submitBtn.addEventListener("click", function () {
      var situation = textarea.value.trim();
      if (!situation) return;

      submitBtn.disabled = true;
      loading.classList.add("visible");
      errorMsg.classList.remove("visible");
      rulingSection.classList.remove("visible");
      var deliberationInterval = startDeliberationAnimation(loading);

      console.log("[arbiter] submit clicked, backendAvailable=" + backendAvailable);

      if (!backendAvailable) {
        console.error("[arbiter] refusing to submit: backend is not available");
        stopDeliberationAnimation(deliberationInterval, loading);
        loading.classList.remove("visible");
        setStatus(false, "Arbiter is unreachable: arbiterd is not running. Rulings cannot be rendered.");
        errorMsg.textContent = "The Arbiter cannot hear your petition: arbiterd is not running.";
        errorMsg.classList.add("visible");
        submitBtn.disabled = false;
        return;
      }

      var headers = {
        "Content-Type": "application/json",
        "X-Session-Token": getSessionToken()
      };
      if (isAdmin) {
        headers["X-Admin-Token"] = ADMIN_SECRET;
      }

      console.log("[arbiter] POST /ruling");
      fetch(API_BASE + "/ruling", {
        method: "POST",
        headers: headers,
        body: JSON.stringify({ situation: situation })
      })
        .then(function (resp) {
          console.log("[arbiter] ruling response HTTP", resp.status);
          if (!resp.ok) {
            throw new Error("HTTP " + resp.status);
          }
          return resp.json();
        })
        .then(function (data) {
          stopDeliberationAnimation(deliberationInterval, loading);
          loading.classList.remove("visible");
          if (data.error) {
            errorMsg.textContent = data.error;
            errorMsg.classList.add("visible");
            submitBtn.disabled = false;
            return;
          }
            rulingText.innerHTML = formatRuling(data.ruling, data.rules);
            rulingSection.classList.add("visible");
            submitBtn.disabled = false;

            var throttleNote = document.getElementById("throttle-note");
            if (data.throttled) {
              throttleNote.textContent = "This ruling was delayed briefly due to high court volume.";
              throttleNote.style.display = "block";
            } else {
              throttleNote.textContent = "";
              throttleNote.style.display = "none";
            }

            // Show "Suggest a Rule" button if deflected
            var suggestBtn = document.getElementById("suggest-btn");
            if (data.deflected) {
              suggestBtn.style.display = "inline-block";
              suggestBtn.setAttribute("data-situation", situation);
              suggestBtn.setAttribute("data-slug", data.slug || "");
            } else {
              suggestBtn.style.display = "none";
            }

            // Update hash for deep link
            if (data.slug) {
              window.history.replaceState(null, '', '#' + data.slug);
            }

            // Reload rulings list from server
            loadRulings(false);
          })
          .catch(function (err) {
            console.error("[arbiter] ruling request failed:", err.message || err);
            stopDeliberationAnimation(deliberationInterval, loading);
            loading.classList.remove("visible");
            setStatus(false, "Arbiter is unreachable: arbiterd is not running. Rulings cannot be rendered.");
            errorMsg.textContent = "The Arbiter could not render a ruling: " + (err.message || "arbiterd is not running.");
            errorMsg.classList.add("visible");
            submitBtn.disabled = false;
          });
    });

    // Suggest button opens modal
    var suggestBtn = document.getElementById("suggest-btn");
    suggestBtn.addEventListener("click", function () {
      var modal = document.getElementById("suggest-modal");
      document.getElementById("suggest-situation").value = suggestBtn.getAttribute("data-situation") || "";
      document.getElementById("suggest-rationale").value = "";
      document.getElementById("suggest-warning").style.display = "none";
      modal.classList.add("visible");
    });

    document.getElementById("suggest-cancel").addEventListener("click", function () {
      document.getElementById("suggest-modal").classList.remove("visible");
    });

    document.getElementById("suggest-submit").addEventListener("click", function () {
      var situation = document.getElementById("suggest-situation").value.trim();
      var rationale = document.getElementById("suggest-rationale").value.trim();
      var warning = document.getElementById("suggest-warning");
      if (!rationale) {
        warning.textContent = "Please explain why this should be a rule.";
        warning.style.display = "block";
        return;
      }
      warning.style.display = "none";

      if (!backendAvailable) {
        console.error("[arbiter] refusing to submit suggestion: backend is not available");
        warning.textContent = "Arbiter is unreachable: arbiterd is not running.";
        warning.style.display = "block";
        return;
      }

      var headers = { "Content-Type": "application/json", "X-Session-Token": getSessionToken() };
      fetch(API_BASE + "/suggest", {
        method: "POST",
        headers: headers,
        body: JSON.stringify({ slug: document.getElementById("suggest-btn").getAttribute("data-slug") || "", rationale: rationale })
      })
        .then(function (resp) {
          if (!resp.ok) {
            throw new Error("HTTP " + resp.status);
          }
          return resp.json();
        })
        .then(function () {
          document.getElementById("suggest-modal").classList.remove("visible");
          loadRulings(false);
        })
        .catch(function (err) {
          console.error("[arbiter] suggestion request failed:", err.message || err);
          warning.textContent = "Could not submit suggestion: " + (err.message || "arbiterd is not running.");
          warning.style.display = "block";
        });
    });

    // Close modal on backdrop click
    document.getElementById("suggest-modal").addEventListener("click", function (e) {
      if (e.target === this) this.classList.remove("visible");
    });

    // Instant tooltip for suggested rules
    var tooltipBox = document.getElementById("tooltip-box");
    document.addEventListener("mouseover", function (e) {
      var li = e.target.closest("li[data-tooltip]");
      if (li) {
        tooltipBox.textContent = li.getAttribute("data-tooltip");
        tooltipBox.style.display = "block";
        var rect = li.getBoundingClientRect();
        tooltipBox.style.left = (rect.left - tooltipBox.offsetWidth - 10) + "px";
        tooltipBox.style.top = rect.top + "px";
      }
    });
    document.addEventListener("mouseout", function (e) {
      var li = e.target.closest("li[data-tooltip]");
      if (li) {
        tooltipBox.style.display = "none";
      }
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
