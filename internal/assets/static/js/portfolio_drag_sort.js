// /manage/portfolio 主页展示表格的拖动排序：
//   基于 HTML5 Drag and Drop，落位后按行序号重排 order（步长 10），
//   对每个变化的行顺序 POST 到 /manage/portfolio/<slug>/order；
//   任何一行失败则刷新页面以拉取权威顺序（避免 UI 与服务端脱节）。
(function () {
  'use strict';

  const table = document.querySelector('.portfolio-featured-table');
  if (!table) return;
  const tbody = table.querySelector('.portfolio-drag-body');
  if (!tbody) return;
  const csrf = table.dataset.csrf || '';

  let dragging = null;

  function snapshot() {
    return Array.from(tbody.querySelectorAll('tr'));
  }

  tbody.addEventListener('dragstart', (e) => {
    const tr = e.target.closest('tr');
    if (!tr || !tbody.contains(tr)) return;
    dragging = tr;
    tr.classList.add('is-dragging');
    // Firefox needs dataTransfer set to initiate drag.
    try { e.dataTransfer.setData('text/plain', tr.dataset.slug || ''); } catch (_) {}
    e.dataTransfer.effectAllowed = 'move';
  });

  tbody.addEventListener('dragend', () => {
    if (dragging) dragging.classList.remove('is-dragging');
    dragging = null;
  });

  tbody.addEventListener('dragover', (e) => {
    if (!dragging) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    const tr = e.target.closest('tr');
    if (!tr || tr === dragging || !tbody.contains(tr)) return;
    const rect = tr.getBoundingClientRect();
    const before = (e.clientY - rect.top) < rect.height / 2;
    if (before) {
      tbody.insertBefore(dragging, tr);
    } else {
      tbody.insertBefore(dragging, tr.nextSibling);
    }
  });

  tbody.addEventListener('drop', (e) => {
    if (!dragging) return;
    e.preventDefault();
    persistOrder();
  });

  async function persistOrder() {
    const rows = snapshot();
    // Compute new orders: 10, 20, 30, ... Compare to data-order; POST only changed.
    const updates = [];
    rows.forEach((tr, i) => {
      const newOrder = (i + 1) * 10;
      const prev = parseInt(tr.dataset.order || '0', 10);
      if (prev !== newOrder) {
        updates.push({ tr, slug: tr.dataset.slug, order: newOrder });
      }
    });
    if (!updates.length) return;

    // Lock the table visually while saving.
    table.classList.add('is-saving');
    try {
      for (const u of updates) {
        const body = new URLSearchParams();
        body.set('order', String(u.order));
        body.set('csrf', csrf);
        const res = await fetch('/manage/portfolio/' + encodeURIComponent(u.slug) + '/order', {
          method: 'POST',
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
          body: body.toString(),
        });
        const data = await res.json().catch(() => ({ ok: false, error: 'bad json' }));
        if (!res.ok || !data.ok) {
          throw new Error(data.error || ('HTTP ' + res.status));
        }
        u.tr.dataset.order = String(u.order);
        const cell = u.tr.querySelector('.portfolio-order-cell');
        if (cell) cell.textContent = String(u.order);
      }
      table.classList.remove('is-saving');
      table.classList.add('is-saved');
      setTimeout(() => table.classList.remove('is-saved'), 800);
    } catch (err) {
      // 顺序 POST 中某一行失败时，前面若干行已在服务端生效，DOM 回滚会
      // 让 UI 与真实 order 脱节。兜底方案：刷新页面重新拉取权威顺序，
      // 让用户清楚看到"部分成功"的现场再决定怎么继续。
      table.classList.remove('is-saving');
      alert('保存排序失败：' + err.message + '\n将刷新页面以同步真实顺序。');
      window.location.reload();
    }
  }
})();
