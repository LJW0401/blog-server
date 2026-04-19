// KaTeX 自动渲染：扫描页面里 $...$ / $$...$$ / \(..\) / \[..\] 分隔符并渲染。
// throwOnError:false → 单个公式写错不会让整页崩，会显示源码加红底提示。
(function () {
  if (typeof renderMathInElement !== 'function') return;
  var opts = {
    delimiters: [
      { left: '$$', right: '$$', display: true },
      { left: '\\[', right: '\\]', display: true },
      { left: '$', right: '$', display: false },
      { left: '\\(', right: '\\)', display: false }
    ],
    throwOnError: false,
    errorColor: '#cc0000'
  };

  // 首屏：对公开文档页和 diary 预览容器生效
  function initial() {
    document.querySelectorAll('.doc-body, .diary-preview, .editor-preview').forEach(function (el) {
      renderMathInElement(el, opts);
    });
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initial);
  } else {
    initial();
  }

  // 暴露给 admin 编辑器：预览 tab 切换后重新渲染新注入的 HTML
  window.renderKatexIn = function (el) {
    if (el) renderMathInElement(el, opts);
  };
})();
