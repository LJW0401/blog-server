// /manage/portfolio 列表页的 inline order 编辑：
//   blur 或 Enter 时 POST 新值到 /manage/portfolio/<slug>/order，
//   成功则原地标记；失败则回滚输入框到原值并显示提示。
(function () {
  'use strict';

  const inputs = document.querySelectorAll('.portfolio-order-input');
  if (!inputs.length) return;

  // Remember the value at focus so we can roll back on error.
  const originals = new WeakMap();

  inputs.forEach((inp) => {
    originals.set(inp, inp.value);
    inp.addEventListener('focus', () => {
      originals.set(inp, inp.value);
    });
    inp.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        inp.blur();
      } else if (e.key === 'Escape') {
        inp.value = originals.get(inp) || '';
        inp.blur();
      }
    });
    inp.addEventListener('blur', async () => {
      const prev = originals.get(inp) || '';
      if (inp.value === prev) return;
      const slug = inp.dataset.slug;
      const csrf = inp.dataset.csrf;
      if (!slug || !csrf) return;
      const body = new URLSearchParams();
      body.set('order', inp.value);
      body.set('csrf', csrf);
      try {
        const res = await fetch('/manage/portfolio/' + encodeURIComponent(slug) + '/order', {
          method: 'POST',
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
          body: body.toString(),
        });
        const data = await res.json().catch(() => ({ ok: false, error: 'bad json' }));
        if (!res.ok || !data.ok) {
          inp.value = prev;
          alert('保存 order 失败：' + (data.error || res.status));
          return;
        }
        originals.set(inp, inp.value);
        inp.classList.add('is-saved');
        setTimeout(() => inp.classList.remove('is-saved'), 800);
      } catch (err) {
        inp.value = prev;
        alert('网络错误：' + err.message);
      }
    });
  });
})();
