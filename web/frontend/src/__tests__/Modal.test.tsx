import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Modal } from '@/components/ui/Modal';

// A dialog that does not hold focus leaves the page behind the overlay
// reachable: Tab walks into the sidebar and Enter activates it, while the user
// believes they are answering the dialog.
describe('Modal focus handling', () => {
  it('keeps Tab inside the dialog', async () => {
    const user = userEvent.setup();

    render(
      <>
        <button>outside before</button>
        <Modal isOpen onClose={() => {}} title="Delete bucket" showCloseButton={false}>
          <button>first</button>
          <button>second</button>
        </Modal>
        <button>outside after</button>
      </>
    );

    const first = screen.getByRole('button', { name: 'first' });
    const second = screen.getByRole('button', { name: 'second' });

    expect(first).toHaveFocus();

    await user.tab();
    expect(second).toHaveFocus();

    // Past the last control, focus wraps to the first rather than leaving.
    await user.tab();
    expect(first).toHaveFocus();

    await user.tab({ shift: true });
    expect(second).toHaveFocus();
  });

  it('returns focus to whatever opened it', async () => {
    const user = userEvent.setup();

    function Harness() {
      const [open, setOpen] = require('react').useState(false);
      return (
        <>
          <button onClick={() => setOpen(true)}>open</button>
          <Modal isOpen={open} onClose={() => setOpen(false)} title="Confirm" showCloseButton={false}>
            <button onClick={() => setOpen(false)}>done</button>
          </Modal>
        </>
      );
    }

    render(<Harness />);
    const opener = screen.getByRole('button', { name: 'open' });

    await user.click(opener);
    expect(screen.getByRole('button', { name: 'done' })).toHaveFocus();

    await user.click(screen.getByRole('button', { name: 'done' }));
    expect(opener).toHaveFocus();
  });

  it('announces itself as a dialog', () => {
    render(
      <Modal isOpen onClose={() => {}} title="Bucket settings" showCloseButton={false}>
        <button>ok</button>
      </Modal>
    );

    const dialog = screen.getByRole('dialog');
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    expect(dialog).toHaveAccessibleName('Bucket settings');
  });
});
