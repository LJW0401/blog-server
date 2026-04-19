(function(){
  var form = document.querySelector('.editor-form');
  if (!form) return;
  var tabs = form.querySelectorAll('.editor-tab');
  var textarea = form.querySelector('textarea[name="body"]');
  var preview = form.querySelector('.editor-preview');
  var label = form.querySelector('.editor-label');
  var csrfInput = form.querySelector('input[name="csrf"]');
  if (!tabs.length || !textarea || !preview || !csrfInput) return;
  var csrf = csrfInput.value;

  function show(mode) {
    tabs.forEach(function(t){
      var on = t.dataset.mode === mode;
      t.classList.toggle('is-active', on);
      t.setAttribute('aria-selected', on ? 'true' : 'false');
    });
    if (mode === 'preview') {
      textarea.hidden = true;
      if (label) label.hidden = true;
      preview.hidden = false;
      preview.textContent = '渲染中…';
      var fd = new URLSearchParams();
      fd.set('csrf', csrf);
      fd.set('body', textarea.value);
      fetch('/manage/docs/preview', {
        method: 'POST',
        body: fd,
        headers: { 'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8' },
        credentials: 'same-origin'
      })
        .then(function(r){ return r.ok ? r.text() : Promise.reject(r.status + ' ' + r.statusText); })
        .then(function(html){ preview.innerHTML = html; })
        .catch(function(err){ preview.textContent = '预览失败：' + err; });
    } else {
      preview.hidden = true;
      textarea.hidden = false;
      if (label) label.hidden = false;
    }
  }
  tabs.forEach(function(t){ t.addEventListener('click', function(){ show(t.dataset.mode); }); });
})();
