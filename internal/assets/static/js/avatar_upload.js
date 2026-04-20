(function () {
  var thumb = document.getElementById('avatar-thumb');
  var file = document.getElementById('avatar-file');
  var urlInput = document.getElementById('avatar-url');
  var status = document.getElementById('avatar-status');
  var del = document.getElementById('avatar-delete');
  if (!thumb || !file || !urlInput) return;
  var csrf = thumb.dataset.csrf || '';

  function setStatus(kind, msg) {
    if (!status) return;
    status.textContent = msg || '';
    status.className = 'avatar-status' + (kind ? ' avatar-status-' + kind : '');
  }

  function renderThumb(src) {
    thumb.innerHTML = '';
    if (src) {
      var img = document.createElement('img');
      img.src = src;
      img.alt = '当前头像';
      thumb.appendChild(img);
      thumb.classList.remove('is-empty');
      if (del) del.hidden = false;
    } else {
      var hint = document.createElement('span');
      hint.className = 'avatar-thumb-hint';
      hint.textContent = '点击上传';
      thumb.appendChild(hint);
      thumb.classList.add('is-empty');
      if (del) del.hidden = true;
    }
  }

  thumb.addEventListener('click', function () { file.click(); });

  file.addEventListener('change', function () {
    if (!file.files || !file.files[0]) return;
    var f = file.files[0];
    if (f.size > 5 * 1024 * 1024) {
      setStatus('err', '文件超过 5MB');
      file.value = '';
      return;
    }
    setStatus('info', '上传中…');
    var fd = new FormData();
    fd.append('csrf', csrf);
    fd.append('avatar', f);
    fetch('/manage/avatar/upload', {
      method: 'POST',
      body: fd,
      credentials: 'same-origin'
    })
      .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, body: j }; }); })
      .then(function (res) {
        if (!res.ok) {
          setStatus('err', (res.body && res.body.error) || '上传失败');
          return;
        }
        var url = res.body && res.body.url;
        if (!url) {
          setStatus('err', '服务端未返回 URL');
          return;
        }
        urlInput.value = url;
        renderThumb(url);
        setStatus('ok', '已上传并保存');
      })
      .catch(function (err) {
        setStatus('err', '网络错误：' + err);
      })
      .finally(function () { file.value = ''; });
  });

  // 如果管理员手动清空 URL 文本框，缩略图联动变空态。
  urlInput.addEventListener('input', function () {
    var v = urlInput.value.trim();
    renderThumb(v);
  });

  if (del) {
    del.addEventListener('click', function () {
      if (!confirm('删除头像？前台将不再显示头像。')) return;
      setStatus('info', '删除中…');
      var fd = new URLSearchParams();
      fd.set('csrf', csrf);
      fetch('/manage/avatar/delete', {
        method: 'POST',
        body: fd,
        headers: { 'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8' },
        credentials: 'same-origin'
      })
        .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, body: j }; }); })
        .then(function (res) {
          if (!res.ok) {
            setStatus('err', (res.body && res.body.error) || '删除失败');
            return;
          }
          urlInput.value = '';
          renderThumb('');
          setStatus('ok', '已删除');
        })
        .catch(function (err) { setStatus('err', '网络错误：' + err); });
    });
  }
})();
