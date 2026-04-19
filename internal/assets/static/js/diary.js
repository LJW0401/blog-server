// /diary 客户端交互
//
// 职责：
//   1. 日历格子点击 → 折叠成当周视图 + 加载当天日记到 textarea
//   2. 自动保存：输入 debounce 1500ms + blur + beforeunload + 切换日期前 flush
//   3. 显式保存：保存按钮 + Ctrl/Cmd+S
//   4. 状态反馈：右上角 "编辑中..." / "已保存于 HH:MM:SS" / "保存失败"
//
// 约定：沿用 mouse-nav.js 的 IIFE + 'use strict' 模式，零全局泄漏；
// 所有与服务器交互的 fetch 都自带 credentials: same-origin + CSRF 校验。
(function () {
  'use strict';

  const DEBOUNCE_MS = 1500;

  // --- DOM 绑定 ---------------------------------------------------------
  const shell = document.querySelector('.diary-shell');
  if (!shell) return; // 不在日记页，不初始化
  const calendar = document.querySelector('.diary-calendar');
  const editor = document.querySelector('.diary-editor');
  const editorDate = document.querySelector('.diary-editor-date');
  const textarea = document.querySelector('.diary-textarea');
  const saveBtn = document.querySelector('.diary-save-btn');
  const deleteBtn = document.querySelector('.diary-delete-btn');
  const promoteBtn = document.querySelector('.diary-promote-btn');
  const status = document.querySelector('.diary-status');

  const csrfMeta = document.querySelector('meta[name="csrf"]');
  const csrf = csrfMeta ? csrfMeta.getAttribute('content') : '';

  // --- 状态机 -----------------------------------------------------------
  // currentDate: 当前编辑的日期 "YYYY-MM-DD"，空表示没选中
  // dirty: textarea 有未保存改动
  // debounceTimer: 当前 debounce 计时器句柄
  let currentDate = '';
  let dirty = false;
  let debounceTimer = null;

  function setStatus(state, text) {
    if (!status) return;
    status.setAttribute('data-state', state);
    status.textContent = text || '';
  }

  function nowStamp() {
    const d = new Date();
    const pad = (n) => String(n).padStart(2, '0');
    return pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
  }

  // --- 日历点击：切到周视图 + 加载当天内容 ------------------------------
  function onCellClick(e) {
    const cell = e.target.closest('.diary-cell');
    if (!cell || cell.classList.contains('diary-out-of-month')) return;
    const date = cell.getAttribute('data-date');
    if (!date) return;
    // 切日期前 flush 当前正在编辑的内容
    flushIfDirty();
    enterWeekMode(date);
    loadDay(date);
  }

  function enterWeekMode(date) {
    // 把日历折叠成当周：找到包含 date 的那一行，其它行隐藏
    const rows = calendar.querySelectorAll('tbody tr');
    rows.forEach((row) => {
      const has = row.querySelector(`[data-date="${date}"]`);
      row.style.display = has ? '' : 'none';
    });
    // 选中标记
    calendar.querySelectorAll('.diary-cell').forEach((c) => {
      c.classList.toggle('diary-cell-selected', c.getAttribute('data-date') === date);
    });
    shell.classList.add('diary-week-mode');
    editor.hidden = false;
    editorDate.textContent = date;
    editorDate.setAttribute('data-date', date);
    currentDate = date;
  }

  async function loadDay(date) {
    setStatus('loading', '加载中...');
    try {
      const res = await fetch('/diary/api/day?date=' + encodeURIComponent(date), {
        credentials: 'same-origin',
      });
      if (!res.ok) throw new Error('http ' + res.status);
      const data = await res.json();
      textarea.value = data.body || '';
      dirty = false;
      setStatus('idle', '');
    } catch (err) {
      setStatus('error', '加载失败');
      console.error('[diary] loadDay', err);
    }
  }

  // --- 保存 -------------------------------------------------------------
  async function saveDay() {
    if (!currentDate) return;
    setStatus('saving', '保存中...');
    const body = new URLSearchParams();
    body.set('date', currentDate);
    body.set('content', textarea.value);
    body.set('csrf', csrf);

    try {
      const res = await fetch('/diary/api/save', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: body.toString(),
      });
      if (!res.ok) throw new Error('http ' + res.status);
      const data = await res.json();
      if (!data.ok) throw new Error(data.error || 'unknown');
      dirty = false;
      setStatus('saved', '已保存于 ' + nowStamp());
    } catch (err) {
      setStatus('error', '保存失败，点击重试');
      console.error('[diary] saveDay', err);
    }
  }

  function flushIfDirty() {
    if (dirty) saveDay();
  }

  // --- 输入事件 ---------------------------------------------------------
  if (textarea) {
    textarea.addEventListener('input', () => {
      dirty = true;
      // "error" 状态粘滞：用户重试成功或手动保存前，不被 "编辑中..." 覆盖
      // (需求 2.3.3：后续输入不会覆盖错误态直到成功)
      if (status && status.getAttribute('data-state') !== 'error') {
        setStatus('editing', '编辑中...');
      }
      if (debounceTimer) clearTimeout(debounceTimer);
      debounceTimer = setTimeout(() => {
        if (dirty) saveDay();
      }, DEBOUNCE_MS);
    });
    textarea.addEventListener('blur', flushIfDirty);
  }

  if (saveBtn) {
    saveBtn.addEventListener('click', saveDay);
  }

  async function deleteDay() {
    if (!currentDate) return;
    // 浏览器原生 confirm —— 需求 2.4.1 明确用这个而不是自定义 modal
    if (!window.confirm('确定要清空 ' + currentDate + ' 的日记？此操作不可恢复')) return;
    setStatus('saving', '删除中...');
    const body = new URLSearchParams();
    body.set('date', currentDate);
    body.set('csrf', csrf);
    try {
      const res = await fetch('/diary/api/delete', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: body.toString(),
      });
      if (!res.ok) throw new Error('http ' + res.status);
      const data = await res.json();
      if (!data.ok) throw new Error(data.error || 'unknown');
      textarea.value = '';
      dirty = false;
      setStatus('saved', '已清空');
      // 整页刷新让月视图绿点重算，最简一致
      window.location.reload();
    } catch (err) {
      setStatus('error', '删除失败，点击重试');
      console.error('[diary] deleteDay', err);
    }
  }

  if (deleteBtn) {
    deleteBtn.addEventListener('click', deleteDay);
  }

  async function promoteDay() {
    if (!currentDate) return;
    // MVP 弹窗：3 个 prompt 依次收集 title / slug / category。后续可换成自定义 modal。
    const title = window.prompt('转正为文档 —— 填入标题：');
    if (title === null) return;
    if (!title.trim()) {
      window.alert('标题不能为空');
      return;
    }
    const slug = window.prompt('输入 slug（小写字母/数字/-，唯一）：');
    if (slug === null) return;
    if (!/^[a-z0-9][a-z0-9-]*$/.test(slug)) {
      window.alert('slug 格式非法：必须以小写字母或数字开头，只含 a–z、0–9、-');
      return;
    }
    const category = window.prompt('输入 category（可留空）：') || '';

    // 先把当前内容存好再转正，避免丢稿
    await saveDay();
    setStatus('saving', '转正中...');
    const body = new URLSearchParams();
    body.set('date', currentDate);
    body.set('title', title.trim());
    body.set('slug', slug);
    body.set('category', category.trim());
    body.set('csrf', csrf);
    try {
      const res = await fetch('/diary/api/promote', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: body.toString(),
      });
      const data = await res.json();
      if (!res.ok || !data.ok) {
        if (data && data.error === 'slug_conflict') {
          setStatus('error', 'slug 冲突');
          window.alert('slug "' + slug + '" 已被占用，请换一个');
        } else {
          setStatus('error', '转正失败');
          window.alert('转正失败：' + (data && data.error ? data.error : res.status));
        }
        return;
      }
      setStatus('saved', '已转正');
      // 跳到新建的文档编辑页，让用户继续打磨
      window.location.href = '/manage/docs/' + encodeURIComponent(data.slug) + '/edit';
    } catch (err) {
      setStatus('error', '转正失败，点击重试');
      console.error('[diary] promoteDay', err);
    }
  }

  if (promoteBtn) {
    promoteBtn.addEventListener('click', promoteDay);
  }

  // Ctrl+S / Cmd+S → 保存（阻止浏览器原生"保存网页为文件"对话框）
  window.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
      if (!editor.hidden) {
        e.preventDefault();
        saveDay();
      }
    }
  });

  // 重试入口：用户点击错误态状态栏时直接再保
  if (status) {
    status.addEventListener('click', () => {
      if (status.getAttribute('data-state') === 'error') saveDay();
    });
  }

  // 关闭页面前 flush（用 sendBeacon 更稳，但 form-encoded 也能被 sendBeacon 接受）
  window.addEventListener('beforeunload', () => {
    if (!dirty || !currentDate) return;
    const body = new URLSearchParams();
    body.set('date', currentDate);
    body.set('content', textarea.value);
    body.set('csrf', csrf);
    try {
      navigator.sendBeacon(
        '/diary/api/save',
        new Blob([body.toString()], { type: 'application/x-www-form-urlencoded' })
      );
    } catch (err) {
      console.error('[diary] beforeunload flush', err);
    }
  });

  // 日历点击委托
  if (calendar) {
    calendar.addEventListener('click', onCellClick);
  }
})();
