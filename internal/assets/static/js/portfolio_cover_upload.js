// 作品集编辑器里的封面上传：
//   点击"上传封面" → 文件选择 → POST /manage/portfolio/cover/upload
//   成功后把返回的 URL 写回 frontmatter 的 cover: 字段。
//   slug 从当前 textarea 的 frontmatter 解析，未填则提示。
(function () {
  'use strict';
  const picker = document.querySelector('.portfolio-cover-picker');
  const btn = document.querySelector('.portfolio-cover-btn');
  const file = document.querySelector('.portfolio-cover-file');
  const status = document.querySelector('.portfolio-cover-status');
  const textarea = document.querySelector('textarea[name="body"]');
  if (!picker || !btn || !file || !textarea) return;
  const csrf = picker.dataset.csrf || '';

  function setStatus(kind, msg) {
    if (!status) return;
    status.textContent = msg || '';
    status.className = 'note portfolio-cover-status' + (kind ? ' is-' + kind : '');
  }

  function parseSlug(body) {
    // Crude but works: find first `slug:` line inside frontmatter block.
    const fmMatch = body.match(/^---\n([\s\S]*?)\n---/);
    if (!fmMatch) return '';
    const m = fmMatch[1].match(/^\s*slug:\s*"?([A-Za-z0-9_-]+)"?/m);
    return m ? m[1] : '';
  }

  function replaceCoverField(body, url) {
    // Replace `cover: ...` inside frontmatter; if missing, insert one after slug:.
    const fmMatch = body.match(/^---\n([\s\S]*?)\n---/);
    if (!fmMatch) return body;
    const fm = fmMatch[1];
    let newFM;
    if (/^\s*cover:/m.test(fm)) {
      newFM = fm.replace(/^\s*cover:.*$/m, 'cover: ' + url);
    } else {
      newFM = fm.replace(/^\s*slug:.*$/m, (m) => m + '\ncover: ' + url);
    }
    return body.replace(fmMatch[0], '---\n' + newFM + '\n---');
  }

  btn.addEventListener('click', () => {
    const slug = parseSlug(textarea.value);
    if (!slug) {
      setStatus('err', '请先在 frontmatter 填好 slug');
      return;
    }
    file.click();
  });

  file.addEventListener('change', async () => {
    if (!file.files || !file.files[0]) return;
    const slug = parseSlug(textarea.value);
    if (!slug) {
      setStatus('err', '请先填 slug');
      file.value = '';
      return;
    }
    const fd = new FormData();
    fd.append('slug', slug);
    fd.append('csrf', csrf);
    fd.append('cover', file.files[0]);
    setStatus('info', '上传中...');
    try {
      const res = await fetch('/manage/portfolio/cover/upload', {
        method: 'POST',
        credentials: 'same-origin',
        body: fd,
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || !data.url) {
        setStatus('err', '上传失败：' + (data.error || res.status));
        file.value = '';
        return;
      }
      textarea.value = replaceCoverField(textarea.value, data.url);
      setStatus('ok', '封面已上传，cover 字段已自动回填');
    } catch (err) {
      setStatus('err', '网络错误：' + err.message);
    }
    file.value = '';
  });
})();
