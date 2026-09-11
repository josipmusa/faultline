import { Trash2 } from 'lucide-react';

import { Button } from './Button';

/* Checked by tsc, not run: the one behaviour of Button worth proving is a
 * type. An icon-only button without an accessible name must not compile. */

export const named = <Button icon={Trash2} aria-label="Delete" />;

export const withText = <Button icon={Trash2}>Delete</Button>;

// @ts-expect-error an icon-only button needs an aria-label
export const unnamed = <Button icon={Trash2} />;
