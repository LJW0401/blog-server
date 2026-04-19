// CSP `script-src 'self'` 下内联 onsubmit="return confirm(...)" 会被静默拦截，
// 这里把相同语义迁到外部脚本：凡带 data-confirm 属性的 form/button，submit 前
// 弹 confirm()，用户取消就 preventDefault 阻止后续提交。
(function () {
  function onSubmit(e) {
    var form = e.target;
    var msg = form.getAttribute('data-confirm');
    if (!msg) return;
    if (!window.confirm(msg)) {
      e.preventDefault();
    }
  }
  document.querySelectorAll('form[data-confirm]').forEach(function (f) {
    f.addEventListener('submit', onSubmit);
  });
})();
