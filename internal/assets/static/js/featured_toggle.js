// 主页展示开关（/manage/docs、/manage/repos 上的"是/否"按钮）的客户端逻辑。
// 目标：点击后页面不动。两层保险，缺一不可：
//   1. 拦截 form submit → fetch POST → 原地翻按钮，避免整页重定向；
//   2. 即便 fetch 拦截没生效（旧 HTML 缓存、扩展拦了 JS、defer 时序意外等），
//      在 submit 当下把 scrollY 落进 sessionStorage；服务端 303 让浏览器重新
//      加载 /manage/docs 后，下面这段 restore 会把页面滚回原位。
//
// 用 capture 阶段在 document 上挂监听，不依赖单个 form 元素是否带某个属性：
// 这样旧缓存 HTML（没 data-featured-toggle）只要 action 路径以 /featured
// 结尾，仍能被识别。
(function () {
  var SCROLL_KEY = "admin_scroll:" + location.pathname;

  // 还原：上一次提交保存的滚动位置。requestAnimationFrame 等浏览器把表格
  // layout 算完，再 scrollTo，避免第一帧 body 高度还没填够导致还原偏小。
  try {
    var saved = sessionStorage.getItem(SCROLL_KEY);
    if (saved !== null) {
      sessionStorage.removeItem(SCROLL_KEY);
      var y = parseFloat(saved);
      if (!isNaN(y)) {
        requestAnimationFrame(function () {
          window.scrollTo(0, y);
        });
      }
    }
  } catch (_) {}

  function isToggleForm(form) {
    if (!form || form.tagName !== "FORM") return false;
    if (form.hasAttribute("data-featured-toggle")) return true;
    var action = form.getAttribute("action") || "";
    return /\/featured$/.test(action);
  }

  function flip(form, nextFeatured) {
    var btn = form.querySelector("button[type=submit]");
    var hidden = form.querySelector('input[name="featured"]');
    if (btn) {
      btn.textContent = nextFeatured ? "是" : "否";
      btn.classList.toggle("link-primary", nextFeatured);
      btn.classList.toggle("link-danger", !nextFeatured);
    }
    if (hidden) hidden.value = nextFeatured ? "false" : "true";
  }

  document.addEventListener(
    "submit",
    function (e) {
      var form = e.target;
      if (!isToggleForm(form)) return;
      // 先存 scroll：fetch 成功就清掉；fetch 失败 / 浏览器走原生回退都会
      // 触发整页重定向，重新加载时由顶部那段 restore 还原。
      try {
        sessionStorage.setItem(SCROLL_KEY, String(window.scrollY));
      } catch (_) {}
      e.preventDefault();
      var btn = form.querySelector("button[type=submit]");
      var hidden = form.querySelector('input[name="featured"]');
      var want = hidden && hidden.value === "true";
      if (btn) btn.disabled = true;
      fetch(form.action, {
        method: "POST",
        body: new FormData(form),
        credentials: "same-origin",
        headers: { Accept: "text/html" },
      })
        .then(function (res) {
          if (!res.ok) throw new Error("HTTP " + res.status);
          flip(form, want);
          // 拦截成功，没发生重定向，把 scroll 槽清掉避免下次 GET 误还原。
          try {
            sessionStorage.removeItem(SCROLL_KEY);
          } catch (_) {}
        })
        .catch(function () {
          // 拦截失败：交还浏览器原生提交，scroll 已经在 storage 里。
          form.submit();
        })
        .finally(function () {
          if (btn) btn.disabled = false;
        });
    },
    true,
  );
})();
