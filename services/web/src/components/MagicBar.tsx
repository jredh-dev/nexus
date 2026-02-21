import { useState, useEffect, useRef, useCallback, useMemo } from 'preact/hooks';

interface Action {
  title: string;
  description: string;
  type: 'navigation' | 'function';
  target: string;
  icon?: string;
}

interface Props {
  /** Render as large centered homepage variant */
  hero?: boolean;
  placeholder?: string;
}

const NAV_ITEMS: Action[] = [
  { title: 'Home', description: 'Go to homepage', type: 'navigation', target: '/', icon: 'fa-house' },
  { title: 'About', description: 'Learn about me', type: 'navigation', target: '/about', icon: 'fa-user' },
  { title: 'Login', description: 'Sign in to your account', type: 'navigation', target: '/login', icon: 'fa-right-to-bracket' },
  { title: 'Sign Up', description: 'Create a new account', type: 'navigation', target: '/signup', icon: 'fa-user-plus' },
  { title: 'Dashboard', description: 'View your dashboard', type: 'navigation', target: '/dashboard', icon: 'fa-gauge' },
  { title: 'Logout', description: 'Sign out', type: 'navigation', target: '/logout', icon: 'fa-right-from-bracket' },
  { title: 'Contact', description: 'Send me an email', type: 'navigation', target: 'mailto:dev@jredh.com', icon: 'fa-envelope' },
];

const DEBOUNCE_MS = 200;

function filterNav(q: string): Action[] {
  const lower = q.toLowerCase();
  return NAV_ITEMS.filter(
    (item) =>
      item.title.toLowerCase().includes(lower) ||
      item.description.toLowerCase().includes(lower)
  );
}

export default function MagicBar({ hero = false, placeholder }: Props) {
  const [query, setQuery] = useState('');
  const [navResults, setNavResults] = useState<Action[]>([]);
  const [apiResults, setApiResults] = useState<Action[]>([]);
  const [selectedIndex, setSelectedIndex] = useState(-1);
  const [isOpen, setIsOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>();

  const allItems = useMemo(() => [...navResults, ...apiResults], [navResults, apiResults]);

  const close = useCallback(() => {
    setIsOpen(false);
    setNavResults([]);
    setApiResults([]);
    setSelectedIndex(-1);
    setLoading(false);
  }, []);

  const searchApi = useCallback(async (q: string) => {
    try {
      setLoading(true);
      const res = await fetch(`/api/actions?q=${encodeURIComponent(q)}`);
      const data: Action[] = await res.json();
      setApiResults(data || []);
    } catch {
      setApiResults([]);
    } finally {
      setLoading(false);
    }
  }, []);

  const executeAction = useCallback((action: Action) => {
    close();
    setQuery('');
    inputRef.current?.blur();
    if (action.type === 'navigation') {
      window.location.href = action.target;
    }
  }, [close]);

  const onInput = useCallback((e: Event) => {
    const value = (e.target as HTMLInputElement).value;
    setQuery(value);

    clearTimeout(debounceRef.current);
    const trimmed = value.trim();
    if (trimmed.length === 0) {
      close();
      return;
    }

    // Instant local nav filtering
    const nav = filterNav(trimmed);
    setNavResults(nav);
    setSelectedIndex(nav.length > 0 ? 0 : -1);
    setIsOpen(true);

    // Debounced API search
    debounceRef.current = setTimeout(() => searchApi(trimmed), DEBOUNCE_MS);
  }, [close, searchApi]);

  const onKeydown = useCallback((e: KeyboardEvent) => {
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        setSelectedIndex((prev) => Math.min(allItems.length - 1, prev + 1));
        break;
      case 'ArrowUp':
        e.preventDefault();
        setSelectedIndex((prev) => Math.max(-1, prev - 1));
        break;
      case 'Enter':
        e.preventDefault();
        if (selectedIndex >= 0 && selectedIndex < allItems.length) {
          executeAction(allItems[selectedIndex]);
        }
        break;
      case 'Escape':
        e.preventDefault();
        close();
        inputRef.current?.blur();
        break;
    }
  }, [allItems, selectedIndex, executeAction, close]);

  // Ctrl+K / Cmd+K global shortcut
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault();
        inputRef.current?.focus();
        inputRef.current?.select();
      }
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, []);

  // Close on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      if (!target.closest('.magic-bar')) {
        close();
      }
    };
    document.addEventListener('click', handler);
    return () => document.removeEventListener('click', handler);
  }, [close]);

  // Scroll selected item into view
  useEffect(() => {
    if (selectedIndex >= 0 && resultsRef.current) {
      const children = resultsRef.current.querySelectorAll('.magic-bar-item');
      children[selectedIndex]?.scrollIntoView({ block: 'nearest' });
    }
  }, [selectedIndex]);

  const wrapperClass = `magic-bar${hero ? ' magic-bar--hero' : ''}`;
  const placeholderText = placeholder || (hero ? 'Where do you want to go?' : 'Search...');

  return (
    <div class={wrapperClass}>
      <i class="fas fa-search magic-bar-icon"></i>
      <input
        ref={inputRef}
        class="magic-bar-input"
        type="text"
        placeholder={placeholderText}
        aria-label="Navigate or search"
        value={query}
        onInput={onInput}
        onKeyDown={onKeydown}
        onFocus={() => {
          if (query.trim().length > 0) {
            const nav = filterNav(query.trim());
            setNavResults(nav);
            setSelectedIndex(nav.length > 0 ? 0 : -1);
            setIsOpen(true);
            searchApi(query.trim());
          }
        }}
        autoComplete="off"
      />
      {!hero && <span class="magic-bar-hint">Ctrl+K</span>}
      <div ref={resultsRef} class={`magic-bar-results${isOpen ? ' visible' : ''}`}>
        {/* Navigation results */}
        {navResults.length > 0 && (
          <div class="magic-bar-section">
            <div class="magic-bar-section-label">Navigate</div>
            {navResults.map((action, i) => (
              <div
                class={`magic-bar-item${i === selectedIndex ? ' selected' : ''}`}
                onMouseEnter={() => setSelectedIndex(i)}
                onClick={() => executeAction(action)}
              >
                <span class="magic-bar-item-icon">
                  <i class={`fas ${action.icon || 'fa-arrow-right'}`}></i>
                </span>
                <span class="magic-bar-item-title">{action.title}</span>
                <span class="magic-bar-item-desc">{action.description}</span>
              </div>
            ))}
          </div>
        )}

        {/* API search results */}
        {apiResults.length > 0 && (
          <div class="magic-bar-section">
            <div class="magic-bar-section-label">Search</div>
            {apiResults.map((action, rawI) => {
              const i = navResults.length + rawI;
              return (
                <div
                  class={`magic-bar-item${i === selectedIndex ? ' selected' : ''}`}
                  onMouseEnter={() => setSelectedIndex(i)}
                  onClick={() => executeAction(action)}
                >
                  <span class="magic-bar-item-icon">
                    {action.type === 'navigation'
                      ? <i class="fas fa-arrow-right"></i>
                      : <i class="fas fa-bolt"></i>}
                  </span>
                  <span class="magic-bar-item-title">{action.title}</span>
                  <span class="magic-bar-item-desc">{action.description}</span>
                </div>
              );
            })}
          </div>
        )}

        {/* Loading indicator */}
        {loading && navResults.length === 0 && apiResults.length === 0 && (
          <div class="magic-bar-empty">Searching...</div>
        )}

        {/* Empty state */}
        {isOpen && !loading && navResults.length === 0 && apiResults.length === 0 && (
          <div class="magic-bar-empty">No results</div>
        )}
      </div>
    </div>
  );
}
