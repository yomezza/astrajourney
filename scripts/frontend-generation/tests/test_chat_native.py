"""Static regression: the integrated chat never embeds a second homepage."""
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

class ChatNativeIntegration(unittest.TestCase):
    def test_no_chat_iframe(self):
        overlay = (ROOT/'mock/ui-overlay.js').read_text('utf-8')
        self.assertNotIn("frame.src='/prototype/chat/index.html'", overlay)
        self.assertNotIn("Modules.create('chat-v13-mock'", overlay)
        self.assertIn('document.body.append(floating)', overlay)
        self.assertIn("await import('/mock/chat-native.js')", overlay)
        self.assertIn("floating.append(document.getElementById('chat-root'))", overlay)
        self.assertIn("stats.after(grid)", overlay)
        self.assertIn("guide.before(button)", overlay)

    def test_chat_fragment_is_not_homepage(self):
        frag=(ROOT/'mock/chat-fragment.html').read_text('utf-8')
        self.assertIn('id="chat-root"',frag)
        self.assertIn('id="chat-emoji-trigger"',frag)
        self.assertIn('id="chat-players-overlay"',frag)
        self.assertIn('id="chat-moderation-overlay"',frag)
        self.assertNotIn('id="game"',frag)
        self.assertNotIn('id="sidebar"',frag)
        self.assertNotIn('<iframe',frag)

    def test_one_dom_root_and_single_window_lifecycle(self):
        code=(ROOT/'mock/chat-native.js').read_text('utf-8')
        self.assertNotIn('Modules.',code)
        self.assertNotIn("'moduleclose'",code)
        self.assertIn("handle.addEventListener('pointerdown'",code)
        self.assertIn("handle.addEventListener('pointermove'",code)
        self.assertNotIn('chat-floating\').style.zIndex',code)
        self.assertIn("(dock?$('chat-home-slot'):$('chat-floating')).append($('chat-root'))",code)
        css=(ROOT/'mock/chat-native.css').read_text('utf-8')
        self.assertNotIn('#window-chat-v13-mock',css)
        self.assertIn('#chat-floating[hidden]',css)

    def test_original_chat_safe_and_studies_untouched(self):
        self.assertTrue((ROOT/'prototype/approved-chat-v13.html').is_file())
        self.assertTrue((ROOT/'prototype/approved-studies-v13-offline.html').is_file())
        self.assertIn('chat-native.js',(ROOT/'README.md').read_text('utf8'))

if __name__ == '__main__': unittest.main()
