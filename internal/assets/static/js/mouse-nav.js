// 鼠标侧键 → 浏览器前进/后退
//
// 背景：大多数桌面浏览器会在 OS 层直接把 mouse button 3 / 4 映射成历史导航，
// 但部分 Linux + 浏览器组合、部分高级鼠标驱动下这个默认会缺失。显式监听
// mouseup + 调 history.back/forward 能让行为在所有环境下一致。
//
// 约定：event.button === 3 是第一个侧键（后退），=== 4 是第二个（前进）。
// 阻止原生 mousedown 是为了避免"浏览器原生处理 + 我们再处理一次"导致的双跳。
(function () {
  'use strict';
  function onDown(e) {
    if (e.button === 3 || e.button === 4) {
      e.preventDefault();
    }
  }
  function onUp(e) {
    if (e.button === 3) {
      e.preventDefault();
      window.history.back();
    } else if (e.button === 4) {
      e.preventDefault();
      window.history.forward();
    }
  }
  window.addEventListener('mousedown', onDown);
  window.addEventListener('mouseup', onUp);
})();
