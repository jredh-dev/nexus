// Magic Bar - universal action search (command palette)
// Activated via Ctrl+K / Cmd+K or by clicking the search input.
(function () {
  'use strict';

  var DEBOUNCE_MS = 150;

  var input, results, selectedIndex, items, debounceTimer;

  // Action executors keyed by function target
  var executors = {
    logout: function () {
      window.location.href = '/logout';
    },
  };

  function init() {
    // Bind to all magic bar instances (primary + clone navbar)
    var bars = document.querySelectorAll('.magic-bar');
    if (bars.length === 0) return;

    // Use the first visible bar as the primary; all inputs share state
    input = document.querySelector('.magic-bar-input');
    results = document.querySelector('.magic-bar-results');
    if (!input || !results) return;

    selectedIndex = -1;
    items = [];

    // Bind all inputs (primary + clone)
    var allInputs = document.querySelectorAll('.magic-bar-input');
    var allResults = document.querySelectorAll('.magic-bar-results');
    for (var i = 0; i < allInputs.length; i++) {
      allInputs[i].addEventListener('input', onInput);
      allInputs[i].addEventListener('keydown', onKeydown);
      allInputs[i].addEventListener('focus', onFocus);
    }

    // Close on outside click
    document.addEventListener('click', function (e) {
      if (!e.target.closest('.magic-bar')) {
        closeAll(allResults);
      }
    });

    // Ctrl+K / Cmd+K to focus the visible input
    document.addEventListener('keydown', function (e) {
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault();
        // Focus the clone navbar input if it's visible, else the primary
        var clone = document.getElementById('navbar-clone');
        var target = input;
        if (clone && clone.classList.contains('is-active')) {
          var cloneInput = clone.querySelector('.magic-bar-input');
          if (cloneInput) target = cloneInput;
        }
        target.focus();
        target.select();
      }
    });
  }

  function onFocus(e) {
    // Set active input/results pair
    var bar = e.target.closest('.magic-bar');
    input = bar.querySelector('.magic-bar-input');
    results = bar.querySelector('.magic-bar-results');

    if (input.value.trim().length > 0) {
      search(input.value.trim());
    }
  }

  function onInput(e) {
    // Ensure we're using this input's bar
    var bar = e.target.closest('.magic-bar');
    input = bar.querySelector('.magic-bar-input');
    results = bar.querySelector('.magic-bar-results');

    clearTimeout(debounceTimer);
    var query = input.value.trim();
    if (query.length === 0) {
      close();
      return;
    }
    debounceTimer = setTimeout(function () {
      search(query);
    }, DEBOUNCE_MS);
  }

  function onKeydown(e) {
    // Ensure correct bar context
    var bar = e.target.closest('.magic-bar');
    input = bar.querySelector('.magic-bar-input');
    results = bar.querySelector('.magic-bar-results');

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        moveSelection(1);
        break;
      case 'ArrowUp':
        e.preventDefault();
        moveSelection(-1);
        break;
      case 'Enter':
        e.preventDefault();
        if (selectedIndex >= 0 && selectedIndex < items.length) {
          executeAction(items[selectedIndex]);
        }
        break;
      case 'Escape':
        e.preventDefault();
        close();
        input.blur();
        break;
    }
  }

  function moveSelection(delta) {
    if (items.length === 0) return;
    selectedIndex = Math.max(-1, Math.min(items.length - 1, selectedIndex + delta));
    renderSelection();
  }

  function renderSelection() {
    var children = results.querySelectorAll('.magic-bar-item');
    for (var i = 0; i < children.length; i++) {
      children[i].classList.toggle('selected', i === selectedIndex);
    }
    if (selectedIndex >= 0 && children[selectedIndex]) {
      children[selectedIndex].scrollIntoView({ block: 'nearest' });
    }
  }

  function search(query) {
    fetch('/api/actions?q=' + encodeURIComponent(query))
      .then(function (res) { return res.json(); })
      .then(function (data) {
        items = data || [];
        selectedIndex = items.length > 0 ? 0 : -1;
        render();
      })
      .catch(function () {
        items = [];
        selectedIndex = -1;
        render();
      });
  }

  function render() {
    if (items.length === 0) {
      results.innerHTML = '<div class="magic-bar-empty">No results</div>';
      results.classList.add('visible');
      return;
    }

    var html = '';
    for (var i = 0; i < items.length; i++) {
      var action = items[i];
      var selectedClass = i === selectedIndex ? ' selected' : '';
      var typeIcon = action.type === 'navigation'
        ? '<i class="fas fa-arrow-right"></i>'
        : '<i class="fas fa-bolt"></i>';
      html += '<div class="magic-bar-item' + selectedClass + '" data-index="' + i + '">';
      html += '<span class="magic-bar-item-icon">' + typeIcon + '</span>';
      html += '<span class="magic-bar-item-title">' + escapeHtml(action.title) + '</span>';
      html += '<span class="magic-bar-item-desc">' + escapeHtml(action.description) + '</span>';
      html += '</div>';
    }
    results.innerHTML = html;
    results.classList.add('visible');

    // Click/hover handlers on items
    var itemEls = results.querySelectorAll('.magic-bar-item');
    for (var j = 0; j < itemEls.length; j++) {
      (function (idx) {
        itemEls[idx].addEventListener('mouseenter', function () {
          selectedIndex = idx;
          renderSelection();
        });
        itemEls[idx].addEventListener('click', function () {
          executeAction(items[idx]);
        });
      })(j);
    }
  }

  function executeAction(action) {
    close();
    input.value = '';
    input.blur();

    if (action.type === 'navigation') {
      window.location.href = action.target;
    } else if (action.type === 'function') {
      var executor = executors[action.target];
      if (executor) {
        executor();
      }
    }
  }

  function close() {
    if (results) {
      results.classList.remove('visible');
      results.innerHTML = '';
    }
    items = [];
    selectedIndex = -1;
  }

  function closeAll(allResults) {
    for (var i = 0; i < allResults.length; i++) {
      allResults[i].classList.remove('visible');
      allResults[i].innerHTML = '';
    }
    items = [];
    selectedIndex = -1;
  }

  function escapeHtml(str) {
    var div = document.createElement('div');
    div.appendChild(document.createTextNode(str));
    return div.innerHTML;
  }

  // Initialize when DOM is ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
